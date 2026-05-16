// Package permissions parses and evaluates the `.agentboard/permissions.yaml`
// rules file that gates write access to workspace paths.
//
// Model:
//
//   - Default is permissive. Absent or empty rules file → every workspace
//     member can write to every path. Rules narrow this default; they
//     don't enable it. This matches wiki convention: open by default,
//     lock specific paths.
//   - Rules are evaluated in declaration order. First matching rule wins
//     for any given path. A rule whose `write` list grants the actor's
//     group → allowed. A rule whose `write` list excludes the actor →
//     denied with a clear error.
//   - Paths with no matching rule stay open (permissive default).
//   - `paths` globs use the `doublestar` syntax (Go library
//     github.com/bmatcuk/doublestar/v4). `**` matches any number of path
//     segments including zero.
//   - `write` is a list of group names. The special name `admins` is
//     synthetic: it always grants every Actor whose role is admin,
//     regardless of the groups table.
//   - **Structural invariant**: writes that touch any path under
//     `.agentboard/` require the Actor to have role admin, regardless
//     of rules. This blocks the "non-admin commits a YAML granting
//     themselves admin, then commits anything" attack.
package permissions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// PermissionsFilePath is where the rules file lives, relative to the
// workspace root. Writes that touch this path are structurally
// admin-only (see package doc).
const PermissionsFilePath = ".agentboard/permissions.yaml"

// SynthAdmins is the synthetic group name that always resolves to
// every user with role admin. Reserved — the groups table cannot
// store a row with this name (see internal/groups.ReservedNames).
const SynthAdmins = "admins"

// AgentboardConfigPrefix is the path prefix that is structurally
// admin-only regardless of YAML rules. The full ban applies to the
// permissions file itself plus any other configuration file we
// might add under `.agentboard/` in the future.
const AgentboardConfigPrefix = ".agentboard/"

// Role mirrors auth.Kind values as a string alias to avoid the
// import cycle (auth doesn't depend on permissions and vice versa).
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleBot    Role = "bot"
)

// Actor is the caller whose write permissions we're evaluating. The
// resolver in handlers_edit / mcp / pre-receive populates this from
// the resolved user + their group memberships.
type Actor struct {
	// Username is the identity. Used only in error messages today.
	Username string
	// Role is the user's kind (admin / member / bot). Bots inherit the
	// role of the user they're attached to via the auth model, so a
	// bot attached to an admin counts as admin here.
	Role Role
	// Groups is the user's group memberships as resolved at request
	// time. Order doesn't matter. The synthetic `admins` group is
	// NOT pre-injected — IsAdmin handles it.
	Groups []string
}

// IsAdmin reports whether the actor's role is admin. Used by the
// structural .agentboard/ check and by the synthetic `admins` group
// resolution.
func (a Actor) IsAdmin() bool {
	return a.Role == RoleAdmin
}

// Rule is one entry in the rules file.
type Rule struct {
	Paths []string `yaml:"paths"`
	Write []string `yaml:"write"`
}

// Rules is the parsed permissions document. A zero-value Rules is
// the open-by-default base — every Allow() returns nil.
type Rules struct {
	Version int    `yaml:"version"`
	Rules   []Rule `yaml:"rules"`
}

// DenyError is returned from Allow when one or more paths are
// disallowed. Callers can errors.As to recover the list of offending
// paths for a structured error response.
type DenyError struct {
	// Username is the actor's username, for context in messages.
	Username string
	// Reason is a short tag: "structural" (touched .agentboard/),
	// "rule" (a YAML rule rejected the actor).
	Reason string
	// Paths is the list of paths that failed.
	Paths []string
}

func (e *DenyError) Error() string {
	prefix := fmt.Sprintf("permission denied for @%s", e.Username)
	if e.Reason == "structural" {
		prefix += " (.agentboard/ is admin-only)"
	}
	if len(e.Paths) == 0 {
		return prefix
	}
	return fmt.Sprintf("%s: %s", prefix, strings.Join(e.Paths, ", "))
}

// IsDenyError is a convenience wrapper around errors.As.
func IsDenyError(err error) (*DenyError, bool) {
	var de *DenyError
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}

// Parse decodes a permissions.yaml body into Rules. An empty input
// returns a zero-value Rules (permissive default).
func Parse(b []byte) (Rules, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return Rules{}, nil
	}
	var r Rules
	if err := yaml.Unmarshal(b, &r); err != nil {
		return Rules{}, fmt.Errorf("permissions: parse yaml: %w", err)
	}
	// Defensive normalization: strip leading slashes and clean up
	// trailing whitespace on every path / write entry. Glob matching
	// elsewhere assumes relative paths.
	for i := range r.Rules {
		paths := make([]string, 0, len(r.Rules[i].Paths))
		for _, p := range r.Rules[i].Paths {
			p = strings.TrimSpace(p)
			p = strings.TrimPrefix(p, "/")
			if p == "" {
				continue
			}
			paths = append(paths, p)
		}
		r.Rules[i].Paths = paths
		writes := make([]string, 0, len(r.Rules[i].Write))
		for _, w := range r.Rules[i].Write {
			w = strings.TrimSpace(w)
			if w == "" {
				continue
			}
			writes = append(writes, w)
		}
		r.Rules[i].Write = writes
	}
	return r, nil
}

// Allow evaluates `actor` against the rules for every path in
// `changedPaths` and returns nil if all are permitted, or a
// *DenyError listing the offending paths.
//
// Evaluation order per path:
//
//  1. Structural check: any change under `.agentboard/` requires
//     actor.IsAdmin(). Collected in DenyError.Reason="structural".
//  2. Rule walk: first matching rule wins. If the rule's `write`
//     intersects actor.Groups (or contains "admins" and actor is
//     admin), allowed. Otherwise denied.
//  3. No match: allowed (permissive default).
//
// Returns nil when changedPaths is empty.
func (r Rules) Allow(actor Actor, changedPaths []string) error {
	structuralDenied := []string{}
	ruleDenied := []string{}

	for _, p := range changedPaths {
		p = strings.TrimPrefix(strings.TrimSpace(p), "/")
		if p == "" {
			continue
		}
		// Structural admin-only check on the config prefix.
		if strings.HasPrefix(p, AgentboardConfigPrefix) {
			if !actor.IsAdmin() {
				structuralDenied = append(structuralDenied, p)
			}
			// Admins still need to pass any rule that explicitly
			// restricts .agentboard/ further — but for the structural
			// check itself, admin passes. Fall through to the rule
			// walk so that the admin's rule eval still applies.
		}
		// Rule walk: first match wins.
		if matched, allowed := r.evaluatePath(actor, p); matched && !allowed {
			ruleDenied = append(ruleDenied, p)
		}
	}

	if len(structuralDenied) > 0 {
		return &DenyError{
			Username: actor.Username,
			Reason:   "structural",
			Paths:    structuralDenied,
		}
	}
	if len(ruleDenied) > 0 {
		return &DenyError{
			Username: actor.Username,
			Reason:   "rule",
			Paths:    ruleDenied,
		}
	}
	return nil
}

// evaluatePath returns (matched, allowed) — matched=false means no
// rule applied to this path (permissive default applies, callers treat
// as allowed). matched=true means a rule did apply; allowed tells the
// outcome.
func (r Rules) evaluatePath(actor Actor, path string) (matched, allowed bool) {
	for _, rule := range r.Rules {
		if !rule.matchesPath(path) {
			continue
		}
		// First match decides. Check whether any of the rule's write
		// names grants the actor.
		return true, rule.permits(actor)
	}
	return false, false
}

// matchesPath reports whether path matches any glob in rule.Paths.
// Errors from doublestar.Match (invalid glob syntax) are treated as
// no-match — the operator who wrote the YAML should fix it; we don't
// want a typo to grant more permission than intended.
func (rule Rule) matchesPath(path string) bool {
	for _, glob := range rule.Paths {
		if ok, _ := doublestar.Match(glob, path); ok {
			return true
		}
	}
	return false
}

// permits reports whether the actor satisfies the rule's `write` list.
func (rule Rule) permits(actor Actor) bool {
	for _, name := range rule.Write {
		if strings.EqualFold(name, SynthAdmins) && actor.IsAdmin() {
			return true
		}
		for _, g := range actor.Groups {
			if strings.EqualFold(name, g) {
				return true
			}
		}
	}
	return false
}

// LoadFromBytes is a thin alias for Parse, intentionally exported so
// callers reading the file via different mechanisms (working-tree mirror,
// git cat-file, etc.) all funnel through the same parser.
func LoadFromBytes(b []byte) (Rules, error) { return Parse(b) }

// LoadForWorktree reads `.agentboard/permissions.yaml` from a workspace's
// working-tree directory. A missing file returns a zero-value Rules
// (permissive default) and a nil error — absence IS a valid configuration
// per the wiki-pivot design.
func LoadForWorktree(worktreeDir string) (Rules, error) {
	full := filepath.Join(worktreeDir, PermissionsFilePath)
	body, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return Rules{}, nil
		}
		return Rules{}, fmt.Errorf("permissions: read %s: %w", full, err)
	}
	return Parse(body)
}
