package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SearchResult is one ranked match. Score is a simple count of distinct
// query-token hits in the haystack, normalized by haystack length —
// good enough for a project with hundreds of items, swappable for
// Bleve later (see spec §14).
type SearchResult struct {
	Key     string  `json:"key"`
	ID      string  `json:"id,omitempty"`
	Shape   string  `json:"shape"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
}

// SearchOpts narrows what gets searched.
type SearchOpts struct {
	Query string // whitespace-separated tokens, case-insensitive substring match
	Scope string // "data" | "" (empty = data) — pages/components added in Phase 2
	Limit int    // default 20
}

// Search performs a ranked substring search across stored data values.
//
// Implementation: walk the catalog, open each file, score by the count
// of query-token occurrences in the value bytes. No tokenization or
// stemming — we match the literal substrings the agent supplies. This
// is intentionally simple; the spec calls out Bleve as the upgrade
// path when scale demands it.
func (s *Store) Search(opts SearchOpts) ([]SearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	tokens := splitQuery(opts.Query)
	if len(tokens) == 0 {
		return nil, nil
	}

	cat := s.Catalog()
	results := make([]SearchResult, 0, opts.Limit*2)

	for _, e := range cat {
		switch e.Shape {
		case ShapeSingleton:
			path := singletonPath(s.dataDir, e.Key)
			if r, ok := scoreFile(path, tokens); ok {
				r.Key = e.Key
				r.Shape = ShapeSingleton
				results = append(results, r)
			}
		case ShapeCollection:
			items, _ := os.ReadDir(collectionDir(s.dataDir, e.Key))
			for _, it := range items {
				if it.IsDir() || !strings.HasSuffix(it.Name(), ".md") {
					continue
				}
				id := strings.TrimSuffix(it.Name(), ".md")
				if r, ok := scoreFile(filepath.Join(collectionDir(s.dataDir, e.Key), it.Name()), tokens); ok {
					r.Key = e.Key
					r.ID = id
					r.Shape = ShapeCollection
					results = append(results, r)
				}
			}
		case ShapeStream:
			// Stream search: scan the active segment for matching lines.
			if r, ok := scoreStreamHead(streamPath(s.dataDir, e.Key), tokens); ok {
				r.Key = e.Key
				r.Shape = ShapeStream
				results = append(results, r)
			}
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return results, nil
}

// splitQuery normalizes the query string into lowercase tokens. Empty
// tokens (caused by extra whitespace) are dropped.
func splitQuery(q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	parts := strings.Fields(q)
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// scoreFile reads an envelope file and scores it against tokens. A
// document with at least one token match makes it into the results;
// the score is the sum of token-occurrence counts. Snippet is a window
// around the first match — cheap heuristic that still beats no context.
func scoreFile(path string, tokens []string) (SearchResult, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SearchResult{}, false
	}
	env, err := UnmarshalEnvelope(b)
	if err != nil {
		return SearchResult{}, false
	}
	hay := strings.ToLower(string(env.Value))
	score := 0.0
	firstAt := -1
	for _, t := range tokens {
		c := strings.Count(hay, t)
		if c > 0 {
			score += float64(c)
			if firstAt == -1 {
				firstAt = strings.Index(hay, t)
			}
		}
	}
	if score == 0 {
		return SearchResult{}, false
	}
	snippet := snippetAround(string(env.Value), firstAt, 80)
	return SearchResult{Score: score, Snippet: snippet}, true
}

// scoreStreamHead scans the active stream segment line-by-line. For
// big streams this is O(file); cheap-enough at our scale and avoids
// touching rotated segments unless the user explicitly asks (which
// they don't in Phase 1 — pagination is for later).
func scoreStreamHead(path string, tokens []string) (SearchResult, bool) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SearchResult{}, false
		}
		return SearchResult{}, false
	}
	defer f.Close()

	score := 0.0
	var firstSnippet string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, streamMaxLineBytes), streamMaxLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		hay := strings.ToLower(line)
		for _, t := range tokens {
			c := strings.Count(hay, t)
			if c > 0 {
				score += float64(c)
				if firstSnippet == "" {
					firstSnippet = snippetAround(line, strings.Index(hay, t), 80)
				}
			}
		}
	}
	if score == 0 {
		return SearchResult{}, false
	}
	return SearchResult{Score: score, Snippet: firstSnippet}, true
}

// snippetAround returns up to `radius` chars on each side of `at`.
// Falls back to the start of the haystack if `at` is invalid. Inserts
// ellipses on truncated edges.
func snippetAround(s string, at, radius int) string {
	if at < 0 {
		at = 0
	}
	start := max(at-radius, 0)
	end := min(at+radius, len(s))
	out := s[start:end]
	out = strings.TrimSpace(out)
	if start > 0 {
		out = "…" + out
	}
	if end < len(s) {
		out = out + "…"
	}
	// JSON values often have trailing newlines / quotes; one round of
	// json.Unmarshal would unescape them, but the cost isn't worth it
	// at the search-snippet layer.
	_ = json.Marshal // keep encoding/json imported for future hooks
	return out
}
