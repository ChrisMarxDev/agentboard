// Package search is the FTS5-backed full-text index over a
// workspace's working-tree mirror. Indexed asynchronously on every
// push; queried by both the JSON API and the server-rendered SSR
// search results page.
//
// Schema (kept dead simple — workspace + path + body):
//
//	CREATE VIRTUAL TABLE search USING fts5(
//	    workspace UNINDEXED,
//	    path,
//	    body,
//	    tokenize = 'unicode61 remove_diacritics 2'
//	);
//
// We intentionally do NOT index every extension; binary files would
// poison the index with garbage tokens. The IndexableExtension allowlist
// caps it to the text-ish formats the dashboard actually renders.
package search

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IndexableExtension reports whether `path`'s extension is one we want
// in the FTS index. Markdown, HTML, JSON, plain text, and ndjson.
// Binaries (images, PDFs) are skipped.
func IndexableExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".mdx", ".html", ".htm", ".json", ".txt", ".ndjson", "":
		return true
	}
	return false
}

// Hit is a single search result.
type Hit struct {
	Workspace string `json:"workspace"`
	Path      string `json:"path"`
	// Snippet is the raw FTS5 snippet with non-HTML sentinel markers
	// around matched terms. Callers HTML-escape this, then swap the
	// sentinels for <mark>/</mark>. Surfaced verbatim in JSON.
	Snippet string `json:"snippet"`
}

// Snippet sentinels are exported so the HTML renderer can swap them
// in for <mark> tags after the snippet body has been escaped.
const (
	SnippetSentinelOpen  = "\x1eHIT\x1e"
	SnippetSentinelClose = "\x1e/HIT\x1e"
)

// internal aliases for QueryContext binding (FTS5 wants strings, not constants).
var (
	snippetSentinelOpen  = SnippetSentinelOpen
	snippetSentinelClose = SnippetSentinelClose
)

// Store wraps the FTS5 virtual table.
type Store struct {
	db *sql.DB
}

// NewStore migrates and returns an index store.
func NewStore(db *sql.DB) (*Store, error) {
	_, err := db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS search USING fts5(
		    workspace UNINDEXED,
		    path,
		    body,
		    tokenize = 'unicode61 remove_diacritics 2'
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("search: migrate FTS5: %w", err)
	}
	return &Store{db: db}, nil
}

// ReindexWorktree wipes the workspace's slice of the index and
// re-walks the working tree at `worktreeRoot`, indexing every
// IndexableExtension file. Cheap on small workspaces; the dogfood
// board has tens of files, not millions.
func (s *Store) ReindexWorktree(ctx context.Context, workspace, worktreeRoot string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM search WHERE workspace = ?`, workspace); err != nil {
		return fmt.Errorf("search: clear workspace: %w", err)
	}

	insert, err := tx.PrepareContext(ctx,
		`INSERT INTO search (workspace, path, body) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insert.Close()

	walkErr := filepath.Walk(worktreeRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries; don't abort the walk
		}
		name := info.Name()
		if info.IsDir() {
			// Skip only AgentBoard's own state directories. Agent-tool
			// homes (.claude, .codex, etc.) are legitimate workspace
			// content and should appear in search results.
			if (name == ".git" || name == ".agentboard") && p != worktreeRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if !IndexableExtension(p) {
			return nil
		}
		// Read at most 1 MiB per file. Anything larger is probably a
		// pasted blob; truncating keeps index bloat in check.
		const maxBytes = 1 << 20
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		if len(data) > maxBytes {
			data = data[:maxBytes]
		}
		rel, err := filepath.Rel(worktreeRoot, p)
		if err != nil {
			return nil
		}
		if _, err := insert.ExecContext(ctx, workspace, rel, string(data)); err != nil {
			return fmt.Errorf("search: insert %s: %w", rel, err)
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	return tx.Commit()
}

// Query searches the FTS index, returning at most `limit` hits ordered
// by FTS rank. The query string is passed through to SQLite's FTS5
// MATCH operator after a light sanitization — we drop characters
// that aren't legal in FTS5 query syntax to avoid surprising errors.
func (s *Store) Query(ctx context.Context, workspace, q string, limit int) ([]Hit, error) {
	q = sanitizeFTSQuery(q)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 25
	}
	// Snippet sentinels are deliberately unusual so the HTML layer can
	// HTML-escape the snippet body (which may legitimately contain
	// `<...>` from indexed HTML pages) and *then* swap the sentinels
	// for real <mark> tags. See snippetSentinel{Open,Close}.
	rows, err := s.db.QueryContext(ctx, `
		SELECT path, snippet(search, 2, ?, ?, '…', 8) AS snip
		FROM search
		WHERE workspace = ? AND search MATCH ?
		ORDER BY rank
		LIMIT ?`, snippetSentinelOpen, snippetSentinelClose, workspace, q, limit)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		h.Workspace = workspace
		if err := rows.Scan(&h.Path, &h.Snippet); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// sanitizeFTSQuery drops characters that aren't safe in an FTS5
// MATCH expression. We keep alphanumerics, whitespace, dot, dash, and
// the explicit MATCH operators (-, +, ", *, AND/OR/NOT). Anything else
// gets replaced with a space. Belt-and-braces against malformed-query
// errors; SQLite will still reject syntactically broken inputs.
func sanitizeFTSQuery(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == ' ', r == '-', r == '.', r == '_',
			r == '"', r == '*':
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.TrimSpace(b.String())
}
