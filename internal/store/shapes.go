package store

// Suggested-shape catalog. Spec §6 + §8: the server stores any
// well-formed payload, never rejects for shape. But silence on a
// drifting shape is unhelpful — when a write looks like it's missing
// fields a downstream component would render against, the response
// includes a non-blocking `shape_hint` warning naming the missing
// fields.
//
// Globs are matched against the path of the leaf being written, NOT
// the disk path — so `tasks/task-42` is a task whether it lives at
// `<root>/content/tasks/task-42.md` or `<root>/data/tasks/task-42.md`.
//
// Adding a shape is a one-liner here plus a new subsection in
// spec.md §8.

import (
	"path/filepath"
	"strings"
)

// ShapeHint pairs a path glob with the frontmatter fields the matching
// suggested shape names. A leaf whose frontmatter is missing one or
// more of those fields gets a `shape_hint` warning on its write
// response. Spec §8 has the full prose definitions.
type ShapeHint struct {
	// Name labels the shape (e.g. "task", "metric", "skill"). Echoed
	// in the warning so the agent can look up the spec section.
	Name string
	// Glob is matched against the path with `path/filepath.Match` —
	// `*` matches one segment, `**` matches multiple. The leading
	// `<root>/content/` or `<root>/data/` is NOT part of the path
	// the glob sees; the leaf path is what the agent sees too.
	Glob string
	// SuggestedFields are the frontmatter keys the matching components
	// expect. Order is preserved in the warning for readability.
	SuggestedFields []string
}

// Shapes is the in-tree path → shape mapping. Keep ≤ 10 fields per
// shape per spec §8. New entries land here only after at least two
// in-the-wild collections converged on the same fields.
var Shapes = []ShapeHint{
	{
		Name:            "task",
		Glob:            "tasks/*",
		SuggestedFields: []string{"title", "status"},
	},
	{
		Name:            "task",
		Glob:            "tasks/**",
		SuggestedFields: []string{"title", "status"},
	},
	{
		Name:            "task",
		Glob:            "*/tasks/*",
		SuggestedFields: []string{"title", "status"},
	},
	{
		Name:            "metric",
		Glob:            "metrics/*",
		SuggestedFields: []string{"value", "label"},
	},
	{
		Name:            "metric",
		Glob:            "metrics/**",
		SuggestedFields: []string{"value", "label"},
	},
	{
		Name:            "metric",
		Glob:            "*/metrics/*",
		SuggestedFields: []string{"value", "label"},
	},
	{
		Name:            "skill",
		Glob:            "skills/*/SKILL",
		SuggestedFields: []string{"name", "description"},
	},
}

// Warning is one non-blocking hint emitted alongside a successful
// write. Per spec §6 + CORE_GUIDELINES §12 (responses are repair
// manuals) every warning includes a code, a message, and a `see`
// pointer to the spec section that explains the shape.
type Warning struct {
	Code                   string   `json:"code"`
	Message                string   `json:"message"`
	See                    string   `json:"see,omitempty"`
	MissingSuggestedFields []string `json:"missing_suggested_fields,omitempty"`
	Shape                  string   `json:"shape,omitempty"`
}

// CheckShape evaluates `path` against the suggested-shape catalog and
// returns warnings for missing fields. Returns nil when no glob
// matches OR every suggested field is present (the success case must
// be silent — agents shouldn't get noise on every write).
//
// `frontmatter` is the parsed user-frontmatter map (already stripped
// of `_meta`). The check is case-sensitive; `Title` ≠ `title`.
func CheckShape(path string, frontmatter map[string]any) []Warning {
	if path == "" {
		return nil
	}
	// Normalize to no leading/trailing slash, no `.md` suffix — same
	// shape the rest of the store uses.
	p := strings.TrimPrefix(path, "/")
	p = strings.TrimSuffix(p, ".md")

	var warnings []Warning
	seen := map[string]bool{}
	for _, hint := range Shapes {
		if seen[hint.Name] {
			// One warning per shape — multiple matching globs
			// (e.g. `tasks/*` AND `tasks/**`) shouldn't produce
			// duplicates.
			continue
		}
		if !globMatch(hint.Glob, p) {
			continue
		}
		var missing []string
		for _, field := range hint.SuggestedFields {
			if _, ok := frontmatter[field]; !ok {
				missing = append(missing, field)
			}
		}
		if len(missing) == 0 {
			seen[hint.Name] = true
			continue
		}
		warnings = append(warnings, Warning{
			Code: "shape_hint",
			Message: "Path looks like a " + hint.Name + " but frontmatter is missing " +
				strings.Join(missing, ", ") +
				". Components rendering this collection (Kanban / Chart / Table) will fall back to defaults; add the suggested field(s) for richer rendering.",
			See:                    "spec.md §8 — Suggested shape: " + hint.Name,
			MissingSuggestedFields: missing,
			Shape:                  hint.Name,
		})
		seen[hint.Name] = true
	}
	return warnings
}

// globMatch is a minimal glob matcher: `*` matches one path segment,
// `**` matches one or more segments. Used by CheckShape against the
// suggested-shape catalog. We don't use path/filepath.Match because
// it doesn't do `**` and we need cross-platform forward-slash matching
// regardless of os.PathSeparator.
func globMatch(pattern, path string) bool {
	pParts := strings.Split(pattern, "/")
	tParts := strings.Split(path, "/")
	return globSegments(pParts, tParts)
}

// globSegments recurses through pattern + target segments. `**` is the
// only branching case (greedy match against zero or more target
// segments). `*` matches exactly one segment. Anything else must
// match literally.
func globSegments(pat, tgt []string) bool {
	for len(pat) > 0 && len(tgt) > 0 {
		switch pat[0] {
		case "**":
			if len(pat) == 1 {
				return true // ** at the end matches the rest
			}
			// Try consuming 0..N target segments.
			for i := 0; i <= len(tgt); i++ {
				if globSegments(pat[1:], tgt[i:]) {
					return true
				}
			}
			return false
		case "*":
			pat = pat[1:]
			tgt = tgt[1:]
		default:
			ok, _ := filepath.Match(pat[0], tgt[0])
			if !ok {
				return false
			}
			pat = pat[1:]
			tgt = tgt[1:]
		}
	}
	// Pattern exhausted but target has leftover → no match unless
	// pattern ends with **.
	if len(pat) == 1 && pat[0] == "**" {
		return true
	}
	return len(pat) == 0 && len(tgt) == 0
}
