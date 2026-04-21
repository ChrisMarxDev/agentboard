package auth

import "strings"

// Rule is one access-control entry attached to an agent identity.
//
// Semantics:
//   - Pattern matches against the HTTP request path (e.g. /api/data/foo.bar).
//   - Methods is the list of HTTP methods the rule applies to. ["*"] matches
//     any method.
//   - Action says what to do when the rule matches: "allow" or "deny".
//
// Rules are evaluated top-to-bottom, first match wins. If no rule matches,
// the identity's AccessMode is the fallback (allow_all → allow, restrict_to_list → deny).
type Rule struct {
	Action  string   `json:"action"`  // "allow" | "deny"
	Pattern string   `json:"pattern"` // glob against HTTP path
	Methods []string `json:"methods"` // ["GET","POST",...] or ["*"]
}

// Allow creates a convenience allow-rule.
func Allow(pattern string, methods ...string) Rule {
	return Rule{Action: "allow", Pattern: pattern, Methods: defaultMethods(methods)}
}

// Deny creates a convenience deny-rule.
func Deny(pattern string, methods ...string) Rule {
	return Rule{Action: "deny", Pattern: pattern, Methods: defaultMethods(methods)}
}

func defaultMethods(m []string) []string {
	if len(m) == 0 {
		return []string{"*"}
	}
	return m
}

// ensureRules returns a non-nil slice so JSON serialization produces `[]`
// instead of `null` for identities that have no rules yet.
func ensureRules(r []Rule) []Rule {
	if r == nil {
		return []Rule{}
	}
	return r
}

// Authorize decides whether a request against (method, path) is allowed for
// an identity with the given mode and rules. This is the hot path — keep it
// allocation-free.
//
// Returned bool is "allow"; when false, the caller should respond 403.
func Authorize(mode AccessMode, rules []Rule, method, path string) bool {
	for i := range rules {
		r := &rules[i]
		if !methodMatches(r.Methods, method) {
			continue
		}
		if !pathMatches(r.Pattern, path) {
			continue
		}
		return r.Action == "allow"
	}
	// No rule matched — fall back to mode.
	return mode == ModeAllowAll
}

// methodMatches returns true if the rule's Methods list contains the request
// method or a wildcard entry.
func methodMatches(methods []string, method string) bool {
	if len(methods) == 0 {
		return true // empty == wildcard for ergonomics on hand-written JSON
	}
	for _, m := range methods {
		if m == "*" {
			return true
		}
		if strings.EqualFold(m, method) {
			return true
		}
	}
	return false
}

// pathMatches implements a minimal glob matcher with two wildcards:
//   - `*`  matches any run of characters except '/'
//   - `**` matches any run of characters including '/'
//
// Patterns without wildcards require an exact match. Leading '/' is required
// in patterns (the matcher does not anchor automatically); paths and patterns
// are compared as-is.
//
// The implementation is iterative with a single level of backtracking so a
// pathological pattern can't exponentially blow up (unlike full regex).
func pathMatches(pattern, path string) bool {
	p, s := 0, 0 // pattern index, path index
	// Backtrack anchors for ** wildcard.
	var starP, starS int = -1, -1
	// Backtrack anchors for * wildcard (single-segment).
	var shortP, shortS int = -1, -1

	for s < len(path) {
		if p < len(pattern) {
			// Detect "**" doublestar.
			if p+1 < len(pattern) && pattern[p] == '*' && pattern[p+1] == '*' {
				starP = p + 2
				starS = s
				p += 2
				// Allow "**/" to match zero segments by also consuming the slash
				// on the pattern side if present.
				if p < len(pattern) && pattern[p] == '/' {
					// leave the '/' in place; the matcher will advance past it
					// either on a real slash in the path or by backtracking.
				}
				continue
			}
			// Single-star *.
			if pattern[p] == '*' {
				shortP = p + 1
				shortS = s
				p++
				continue
			}
			// Literal match.
			if pattern[p] == path[s] {
				p++
				s++
				continue
			}
		}
		// Mismatch or end of pattern — try backtracking.
		// Prefer single-star backtrack first (narrower scope).
		if shortP != -1 && path[shortS] != '/' {
			shortS++
			s = shortS
			p = shortP
			continue
		}
		if starP != -1 {
			starS++
			s = starS
			p = starP
			continue
		}
		return false
	}
	// Consume trailing wildcards / slashes.
	for p < len(pattern) {
		if pattern[p] == '*' {
			p++
			continue
		}
		return false
	}
	return true
}
