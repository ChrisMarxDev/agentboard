package mdx

import (
	"database/sql"
	"fmt"
	"strings"
)

// SearchHit is one match in a full-text search response.
//
// Shape is wire-stable — agents decide whether a page is worth reading
// from the returned summary + tags + snippet + writer attribution, often
// without fetching the body. Adding fields is fine; removing or renaming
// is a breaking change to the MCP surface.
type SearchHit struct {
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	Tags      []string `json:"tags"`
	Snippet   string   `json:"snippet"`
	Rank      float64  `json:"rank"` // lower = better (BM25)
	Writer    string   `json:"writer"`
	UpdatedAt string   `json:"updated_at"`
}

// SearchStore is an FTS5 index over every markdown page in the project.
// Sibling of MetaStore — rides on the same *sql.DB the data store uses, its
// own virtual table so no collisions.
//
// The index holds authored frontmatter (summary, tags) alongside title and
// source so agent-written metadata gets weighted higher than body prose.
// This is how we get "search for 'blog post' finds a page called
// 'social voice guidelines'" without a backend LLM or embeddings —
// the authoring agent mentions use cases in the summary and FTS does
// the rest.
type SearchStore struct {
	db   *sql.DB
	meta *MetaStore
}

// currentSchema is the column list we expect on pages_fts. If the existing
// table doesn't match (i.e. pre-summary/tags builds), NewSearchStore drops
// and recreates the table — pages_fts is a pure index, Rebuild from disk
// repopulates it immediately afterwards.
const currentSchemaCols = "path,title,summary,tags,source"

// NewSearchStore opens (and creates if needed) the pages_fts virtual table.
// Returns an error — unlike MetaStore — because a misconfigured FTS build
// is silent-serious: we want the server to fail loudly rather than
// degrade to zero-hit search later.
//
// When meta is non-nil, Query enriches each hit with the last-writer and
// timestamp pulled from page_meta. Pass nil on bare-bones boots where
// page_meta isn't wired up; hits just omit attribution.
func NewSearchStore(db *sql.DB, meta *MetaStore) (*SearchStore, error) {
	// Drop any pre-existing pages_fts whose schema predates summary/tags.
	// Safe because the table is an index rebuilt from disk on every boot.
	if stale, err := schemaOutdated(db); err != nil {
		return nil, fmt.Errorf("inspect pages_fts: %w", err)
	} else if stale {
		if _, err := db.Exec(`DROP TABLE IF EXISTS pages_fts`); err != nil {
			return nil, fmt.Errorf("drop pages_fts: %w", err)
		}
	}

	_, err := db.Exec(`
	CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
		path UNINDEXED,
		title,
		summary,
		tags,
		source,
		tokenize='unicode61 remove_diacritics 2'
	);
	`)
	if err != nil {
		return nil, fmt.Errorf("create pages_fts: %w", err)
	}
	return &SearchStore{db: db, meta: meta}, nil
}

// schemaOutdated returns true if pages_fts exists with an older column set
// than currentSchemaCols. Only introspects sqlite_master; no row scans.
func schemaOutdated(db *sql.DB) (bool, error) {
	var createSQL sql.NullString
	err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='pages_fts'`,
	).Scan(&createSQL)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !createSQL.Valid {
		return false, nil
	}
	// Cheap substring check on the stored CREATE sql — each column appears
	// verbatim in the `CREATE VIRTUAL TABLE … fts5( … )` body.
	for _, col := range strings.Split(currentSchemaCols, ",") {
		if !strings.Contains(createSQL.String, col) {
			return true, nil
		}
	}
	return false, nil
}

// IndexPage upserts a page's content into the FTS index. Called after a
// successful page write. summary and tags come from the page's frontmatter
// (zero-values when the author didn't fill them in — search still works,
// just with less signal for that page).
func (s *SearchStore) IndexPage(path, title, summary string, tags []string, source string) error {
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
		`INSERT INTO pages_fts (path, title, summary, tags, source) VALUES (?, ?, ?, ?, ?)`,
		path, title, summary, joinTags(tags), source,
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
	stmt, err := tx.Prepare(
		`INSERT INTO pages_fts (path, title, summary, tags, source) VALUES (?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range pages {
		if _, err := stmt.Exec(p.Path, p.Title, p.Summary, joinTags(p.Tags), p.Source); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Query runs a full-text search against title + summary + tags + source.
// Columns are BM25-weighted (title 3x, summary 2x, tags 2x, source 1x) so
// a match in an agent-authored summary outranks a match buried in the body.
// An empty query returns no hits rather than failing.
//
// tagsFilter is an optional OR-set: when non-empty, hits must contain at
// least one of the listed tags. This is additive to the main query — the
// agent can search with a single shared interface whether filtering by
// topic or not.
//
// When the store was constructed with a MetaStore, each hit is enriched
// with the last writer + timestamp from page_meta. Otherwise those fields
// stay empty.
func (s *SearchStore) Query(q string, tagsFilter []string, limit int) ([]SearchHit, error) {
	q = strings.TrimSpace(q)
	if q == "" && len(tagsFilter) == 0 {
		return []SearchHit{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	fts := buildFTSQuery(q, tagsFilter)

	// BM25 weights: title 3x, summary 2x, tags 2x, source 1x. Path is
	// UNINDEXED so it doesn't count. Weights apply in schema column order
	// over indexed columns only.
	// Snippet uses column -1 to auto-pick whichever column matched best.
	rows, err := s.db.Query(`
		SELECT path, title, summary, tags,
		       snippet(pages_fts, -1, '<mark>', '</mark>', '…', 12) AS snippet,
		       bm25(pages_fts, 3.0, 2.0, 2.0, 1.0) AS rank
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
		var tagsStr string
		if err := rows.Scan(&h.Path, &h.Title, &h.Summary, &tagsStr, &h.Snippet, &h.Rank); err != nil {
			return nil, err
		}
		h.Tags = splitTags(tagsStr)
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Enrich with writer + updated_at. One Get per hit — page_meta is
	// tiny, SQLite is local, 20 roundtrips is microseconds. Avoids the
	// path-normalization headache of a SQL JOIN (page_meta stores
	// "features/auth" while pages_fts stores "/features/auth", and
	// the root is "index" vs "/").
	if s.meta != nil {
		for i := range out {
			if m, _ := s.meta.Get(ftsPathToMetaPath(out[i].Path)); m != nil {
				out[i].Writer = m.LastActor
				out[i].UpdatedAt = m.LastAt
			}
		}
	}
	return out, nil
}

// ftsPathToMetaPath maps the URL-shaped path stored in pages_fts to the
// slug-shaped path stored in page_meta:
//
//	"/"               → "index"
//	"/features/auth"  → "features/auth"
//
// See handlers_pages.go for the corresponding write-side normalization.
func ftsPathToMetaPath(p string) string {
	if p == "/" {
		return "index"
	}
	return strings.TrimPrefix(p, "/")
}

// joinTags flattens a tag list into a space-separated string suitable for
// the FTS tags column. Tokens containing spaces or punctuation are skipped
// rather than quoted — tag authors are expected to use kebab-case slugs.
func joinTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	cleaned := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		cleaned = append(cleaned, t)
	}
	return strings.Join(cleaned, " ")
}

// splitTags is the inverse of joinTags — turns the stored space-separated
// string back into a slice for the response.
func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

// buildFTSQuery composes the MATCH expression from a free-text query and
// an optional tag filter:
//
//   - Free-text: each whitespace-separated term is double-quoted so casual
//     input like "how do grab picks work" doesn't confuse FTS5's parser.
//     Pre-quoted queries pass through untouched.
//   - Tag filter: OR-ed set, scoped with FTS5 column syntax `tags:(a OR b)`.
//     AND-joined with the free-text portion.
//
// An empty free-text query with tags-only filter yields `tags:(a OR b)`.
func buildFTSQuery(q string, tagsFilter []string) string {
	text := buildTextMatch(q)
	tags := buildTagsMatch(tagsFilter)
	switch {
	case text != "" && tags != "":
		return tags + " AND " + text
	case text != "":
		return text
	case tags != "":
		return tags
	default:
		return `""`
	}
}

func buildTextMatch(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	if strings.Contains(q, `"`) {
		return q
	}
	parts := strings.Fields(q)
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.Trim(p, ".,;:!?()[]{}")
		if trimmed == "" {
			continue
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(trimmed, `"`, `""`)+`"`)
	}
	if len(quoted) == 0 {
		return ""
	}
	return strings.Join(quoted, " ")
}

func buildTagsMatch(tags []string) string {
	cleaned := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		cleaned = append(cleaned, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	if len(cleaned) == 0 {
		return ""
	}
	if len(cleaned) == 1 {
		return "tags:" + cleaned[0]
	}
	return "tags:(" + strings.Join(cleaned, " OR ") + ")"
}
