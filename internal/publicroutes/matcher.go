// Package publicroutes implements glob-based path matching for
// config-driven public read access. Writes always require auth — the
// matcher never considers the HTTP method except to refuse writes.
//
// Glob semantics:
//
//	/foo         exact match
//	/foo/*       direct children only (one segment)
//	/foo/**      all descendants, any depth
//	!/foo/bar    exclusion (applied after includes; last match wins)
//
// A path is publicly readable iff it matches at least one include pattern
// AND no exclusion pattern matches. Matching is prefix-aware — trailing
// slashes on the request path are normalised away before comparison.
package publicroutes

import (
	"net/http"
	"strings"
)

// Matcher compiles a list of path patterns once and matches requests
// against them in O(n) per request where n is the number of patterns.
// The typical config has well under 20 patterns so this is fine.
type Matcher struct {
	includes []compiled
	excludes []compiled
}

type compiled struct {
	raw   string
	parts []string // segment list; "*" and "**" stay literal
}

// New builds a Matcher from raw YAML patterns. Patterns starting with `!`
// are exclusions. Empty patterns are skipped. Malformed patterns fall
// back to exact matching on the raw string.
func New(patterns []string) *Matcher {
	m := &Matcher{}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		neg := false
		if strings.HasPrefix(p, "!") {
			neg = true
			p = p[1:]
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		c := compiled{raw: p, parts: splitPath(p)}
		if neg {
			m.excludes = append(m.excludes, c)
		} else {
			m.includes = append(m.includes, c)
		}
	}
	return m
}

// HasRules reports whether any include pattern was configured. Used by
// the boot-time check that avoids spinning up the public middleware when
// the config is empty — keeps the hot path clean.
func (m *Matcher) HasRules() bool { return len(m.includes) > 0 }

// IsPubliclyReadable reports whether a GET/HEAD/OPTIONS request to
// `path` should skip auth. Always returns false for write methods, no
// matter what the patterns say — this is the writes_require_auth
// invariant enforced in code, not config.
func (m *Matcher) IsPubliclyReadable(method, path string) bool {
	if !isReadMethod(method) {
		return false
	}
	if path == "" {
		return false
	}
	path = normalisePath(path)
	parts := splitPath(path)
	matched := false
	for _, inc := range m.includes {
		if matchParts(inc.parts, parts) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	for _, exc := range m.excludes {
		if matchParts(exc.parts, parts) {
			return false
		}
	}
	return true
}

func isReadMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// splitPath returns the slash-separated segments of a path. Leading
// slashes produce an empty first segment, which callers ignore.
func splitPath(p string) []string {
	out := strings.Split(p, "/")
	// Drop leading "" produced by the leading slash; keeps segment
	// counts aligned between pattern and path.
	if len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	// Drop trailing "" produced by trailing slashes so "/foo" and
	// "/foo/" are equivalent.
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func normalisePath(p string) string {
	if p == "/" || p == "" {
		return "/"
	}
	if strings.HasSuffix(p, "/") {
		return p[:len(p)-1]
	}
	return p
}

// matchParts walks pattern and path segments in parallel. `*` matches
// exactly one segment; `**` matches zero or more segments (non-greedy
// over the remaining pattern, implemented as a classic backtracking
// walk). Case-sensitive.
func matchParts(pat, path []string) bool {
	pi, si := 0, 0
	// Track the last wildcard position to support backtracking for `**`.
	starPi, starSi := -1, 0
	for si < len(path) {
		if pi < len(pat) {
			switch pat[pi] {
			case "**":
				// `**` can match any number of path segments. Record the
				// position; on mismatch below, retry with one more segment
				// consumed.
				starPi = pi
				starSi = si
				pi++
				continue
			case "*":
				// Single-segment wildcard.
				pi++
				si++
				continue
			default:
				if pat[pi] == path[si] {
					pi++
					si++
					continue
				}
			}
		}
		// Mismatch: backtrack into the last `**` if there was one.
		if starPi >= 0 {
			starSi++
			pi = starPi + 1
			si = starSi
			continue
		}
		return false
	}
	// Consume any trailing `**` in the pattern.
	for pi < len(pat) && pat[pi] == "**" {
		pi++
	}
	return pi == len(pat)
}
