package mdx

import (
	"database/sql"
	"fmt"
	"strings"
)

// SearchHit is one match in a full-text search response.
type SearchHit struct {
	Path    string  `json:"path"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Rank    float64 `json:"rank"` // lower = better (BM25)
}

// SearchStore is an FTS5 index over every markdown page in the project.
// Sibling of MetaStore — rides on the same *sql.DB the data store uses, its
// own virtual table so no collisions.
type SearchStore struct {
	db *sql.DB
}

// NewSearchStore opens (and creates if needed) the pages_fts virtual table.
// Returns an error — unlike MetaStore — because a misconfigured FTS build
// is silent-serious: we want the server to fail loudly rather than
// degrade to zero-hit search later.
func NewSearchStore(db *sql.DB) (*SearchStore, error) {
	_, err := db.Exec(`
	CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
		path UNINDEXED,
		title,
		source,
		tokenize='unicode61 remove_diacritics 2'
	);
	`)
	if err != nil {
		return nil, fmt.Errorf("create pages_fts: %w", err)
	}
	return &SearchStore{db: db}, nil
}

// IndexPage upserts a page's content into the FTS index. Called after a
// successful page write.
func (s *SearchStore) IndexPage(path, title, source string) error {
	// FTS5 doesn't support ON CONFLICT on UNINDEXED columns, so do a
	// DELETE-then-INSERT inside a tx.
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM pages_fts WHERE path = ?`, path); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO pages_fts (path, title, source) VALUES (?, ?, ?)`,
		path, title, source,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePage drops a page from the FTS index.
func (s *SearchStore) DeletePage(path string) error {
	_, err := s.db.Exec(`DELETE FROM pages_fts WHERE path = ?`, path)
	return err
}

// Rebuild wipes + repopulates the index from the given pages. Called on
// server startup so a fresh database picks up existing content, and
// available as a fallback if the index ever drifts.
func (s *SearchStore) Rebuild(pages []PageInfo) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM pages_fts`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO pages_fts (path, title, source) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range pages {
		if _, err := stmt.Exec(p.Path, p.Title, p.Source); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Query runs a full-text search against title + source. Returns at most
// `limit` hits ordered by BM25 rank (best first). An empty query returns
// no hits rather than failing — the frontend can call Query unconditionally.
//
// The user's query is passed through a small sanitizer that quotes each
// whitespace-delimited term. This turns `foo bar` into `"foo" "bar"` which
// FTS5 treats as an implicit AND — intuitive default for multi-word search.
// FTS5 operators (NEAR, OR, NOT, prefix-*) still pass through when the
// user includes quotes themselves.
func (s *SearchStore) Query(q string, limit int) ([]SearchHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []SearchHit{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	fts := buildFTSQuery(q)

	rows, err := s.db.Query(`
		SELECT path, title,
		       snippet(pages_fts, 2, '<mark>', '</mark>', '…', 12) AS snippet,
		       rank
		FROM pages_fts
		WHERE pages_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, fts, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]SearchHit, 0, limit)
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.Path, &h.Title, &h.Snippet, &h.Rank); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// buildFTSQuery wraps each whitespace-separated term in double quotes so a
// casual "how do grab picks work" doesn't confuse FTS5's MATCH parser with
// stray punctuation. If the user writes a pre-quoted query ("exact phrase"),
// we pass it through untouched.
func buildFTSQuery(q string) string {
	if strings.Contains(q, `"`) {
		return q
	}
	parts := strings.Fields(q)
	if len(parts) == 0 {
		return `""`
	}
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		// Strip trailing punctuation that would break the tokenizer
		// ("foo." → "foo"), and skip empties.
		trimmed := strings.Trim(p, ".,;:!?()[]{}")
		if trimmed == "" {
			continue
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(trimmed, `"`, `""`)+`"`)
	}
	if len(quoted) == 0 {
		return `""`
	}
	return strings.Join(quoted, " ")
}
