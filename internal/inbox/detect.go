package inbox

import (
	"regexp"
	"strings"
)

// mentionRE matches @username embedded in free text. Keep the grammar
// in lockstep with the frontend <RichText> component and the username
// regex in internal/auth — this is the only server-side place that
// pulls mentions out of prose.
//
// Rules:
//   - `@username` at start of string or preceded by whitespace / `(`
//   - `username` matches [a-z][a-z0-9_-]{0,31}
//   - Followed by end of string, whitespace, or standard punctuation
//
// Mid-word `@` (like `user@host.com`) doesn't match — the `@` must be
// preceded by a mention-anchor character. The trailing boundary is
// enforced by the regex engine matching greedily up to 32 chars and
// only accepting the tail set.
var mentionRE = regexp.MustCompile(`(?:^|[\s(])@([a-z][a-z0-9_-]{0,31})(?:[\s.,!?;:)]|$)`)

// ExtractMentions returns the unique set of usernames referenced in
// `text`. Deduplicated, lower-cased, in first-seen order.
//
// Dedupe matters: a paragraph with "@alice opened a PR; @alice moved
// it to review" should produce one inbox item, not two.
func ExtractMentions(text string) []string {
	if text == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, m := range mentionRE.FindAllStringSubmatch(text, -1) {
		name := strings.ToLower(m[1])
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// ExtractMentionsInAny walks an arbitrary JSON-y value looking for
// @username patterns in any string it sees. Used to scan data values
// that may be strings or nested objects/arrays (e.g. kanban card
// titles inside an array of objects).
//
// Returns the union across all string leaves, deduplicated in the
// iteration order we happen to encounter them.
func ExtractMentionsInAny(v any) []string {
	seen := map[string]struct{}{}
	var out []string
	walk(v, func(s string) {
		for _, name := range ExtractMentions(s) {
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				out = append(out, name)
			}
		}
	})
	return out
}

func walk(v any, visit func(string)) {
	switch x := v.(type) {
	case string:
		visit(x)
	case []any:
		for _, item := range x {
			walk(item, visit)
		}
	case map[string]any:
		for _, val := range x {
			walk(val, visit)
		}
	}
}

// DiffAssigneesAny finds newly-added usernames between `prev` and
// `next`, both of which are expected to be string arrays (the
// convention for kanban cards' `assignees` field). Returns new
// entries in `next` that weren't in `prev`, lower-cased and
// deduplicated. Silently ignores anything that isn't a string.
//
// Used when an array row is PATCHed to detect who's now on the hook.
func DiffAssigneesAny(prev, next any) []string {
	prevSet := map[string]struct{}{}
	for _, name := range stringSlice(prev) {
		prevSet[strings.ToLower(name)] = struct{}{}
	}
	var added []string
	seen := map[string]struct{}{}
	for _, raw := range stringSlice(next) {
		name := strings.ToLower(raw)
		if name == "" {
			continue
		}
		if _, ok := prevSet[name]; ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		added = append(added, name)
	}
	return added
}

func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
