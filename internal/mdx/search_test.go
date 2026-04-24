package mdx

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// newTestStores returns a fresh in-memory SQLite with both stores wired up.
// meta is optional — pass nil to skip attribution enrichment in tests.
func newTestStores(t *testing.T, withMeta bool) (*SearchStore, *MetaStore) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	var meta *MetaStore
	if withMeta {
		ms, err := NewMetaStore(db)
		if err != nil {
			t.Fatalf("meta store: %v", err)
		}
		meta = ms
	}

	search, err := NewSearchStore(db, meta)
	if err != nil {
		t.Fatalf("search store: %v", err)
	}
	return search, meta
}

func TestSearch_SummaryOutranksBody(t *testing.T) {
	// The KB pitch: a page titled "social voice guidelines" whose body never
	// says "blog" should still surface when an agent searches for "blog" —
	// because the authoring agent mentioned "blog posts" in the summary.
	s, _ := newTestStores(t, false)

	if err := s.IndexPage("/voice", "Social Voice Guidelines",
		"Voice and tone for blog posts, social media, and newsletters.",
		[]string{"voice", "content"},
		"Keep it friendly. Short sentences. First person when appropriate."); err != nil {
		t.Fatalf("index voice: %v", err)
	}
	if err := s.IndexPage("/deploy", "Deploy Runbook", "",
		[]string{"ops"},
		"Steps to deploy. Run `task build`. Check logs."); err != nil {
		t.Fatalf("index deploy: %v", err)
	}

	hits, err := s.Query("blog", nil, 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit for 'blog', got %d: %+v", len(hits), hits)
	}
	if hits[0].Path != "/voice" {
		t.Errorf("expected /voice hit, got %q", hits[0].Path)
	}
	if hits[0].Summary == "" {
		t.Errorf("summary should be populated in hit")
	}
	if len(hits[0].Tags) != 2 {
		t.Errorf("tags should round-trip, got %v", hits[0].Tags)
	}
}

func TestSearch_TitleOutranksBody(t *testing.T) {
	// BM25 weights put title matches above body matches even when the body
	// has more hits. Verifies the 3x title weight actually applies.
	// unicode61 tokenizer doesn't stem, so we use the exact token in
	// both title and body.
	s, _ := newTestStores(t, false)

	_ = s.IndexPage("/primary", "Authentication", "", nil,
		"We use tokens for everything. Tokens are stored in SQLite.")
	_ = s.IndexPage("/other", "Other Page", "", nil,
		"This page covers authentication in one sentence as an aside.")

	hits, err := s.Query("authentication", nil, 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("expected 2 hits, got %d: %+v", len(hits), hits)
	}
	if hits[0].Path != "/primary" {
		t.Errorf("title match /primary should rank first, got order: %q then %q",
			hits[0].Path, hits[1].Path)
	}
}

func TestSearch_TagsFilter(t *testing.T) {
	s, _ := newTestStores(t, false)

	_ = s.IndexPage("/voice", "Voice Guide", "Tone guidance.",
		[]string{"voice"}, "content here")
	_ = s.IndexPage("/deploy", "Deploy Runbook", "How to deploy.",
		[]string{"ops"}, "content here")
	_ = s.IndexPage("/onboard", "Onboarding", "How new hires ramp up.",
		[]string{"ops", "people"}, "content here")

	// Tags-only query (empty q) — valid, returns every page with the tag.
	hits, err := s.Query("", []string{"ops"}, 10)
	if err != nil {
		t.Fatalf("tags-only query: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 ops hits, got %d: %+v", len(hits), hits)
	}

	// Mixed q + tags.
	hits, err = s.Query("deploy", []string{"ops"}, 10)
	if err != nil {
		t.Fatalf("mixed query: %v", err)
	}
	if len(hits) != 1 || hits[0].Path != "/deploy" {
		t.Fatalf("expected only /deploy, got %+v", hits)
	}

	// Tag mismatch filters the hit out.
	hits, err = s.Query("deploy", []string{"voice"}, 10)
	if err != nil {
		t.Fatalf("tag-mismatch query: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits when tag doesn't match, got %+v", hits)
	}
}

func TestSearch_EmptyQueryReturnsEmpty(t *testing.T) {
	s, _ := newTestStores(t, false)
	_ = s.IndexPage("/a", "A", "", nil, "body")

	hits, err := s.Query("", nil, 10)
	if err != nil {
		t.Fatalf("empty query: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("empty query + no tags should return no hits, got %v", hits)
	}
}

func TestSearch_WriterEnrichment(t *testing.T) {
	s, meta := newTestStores(t, true)

	_ = s.IndexPage("/voice", "Voice Guide", "Tone guidance.",
		[]string{"voice"}, "body content")
	// page_meta uses the slug path ("voice"), FTS uses the URL path ("/voice").
	// The store normalizes across them on lookup — this test guards the mapping.
	if err := meta.Record("voice", "alice"); err != nil {
		t.Fatalf("meta.Record: %v", err)
	}

	hits, err := s.Query("tone", nil, 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].Writer != "alice" {
		t.Errorf("writer not enriched: got %q, want %q", hits[0].Writer, "alice")
	}
	if hits[0].UpdatedAt == "" {
		t.Errorf("updated_at should be populated from meta")
	}
}

func TestSearch_RootPageMetaMapping(t *testing.T) {
	// The root page is stored as "/" in FTS but "index" in page_meta.
	// ftsPathToMetaPath handles this asymmetry; regression-test it.
	s, meta := newTestStores(t, true)

	_ = s.IndexPage("/", "Home", "The project homepage.",
		nil, "welcome to the dashboard")
	_ = meta.Record("index", "bob")

	hits, err := s.Query("homepage", nil, 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].Writer != "bob" {
		t.Errorf("root-page writer lookup failed: got %q", hits[0].Writer)
	}
}

func TestSearch_SchemaUpgrade(t *testing.T) {
	// Simulate an older deployment by creating the pre-summary/tags FTS
	// schema, then construct a fresh SearchStore and verify the upgrade
	// path drops + recreates the table cleanly. Without the drop, the
	// first INSERT would fail ("table pages_fts has 3 columns but 5
	// values were supplied").
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE VIRTUAL TABLE pages_fts USING fts5(path UNINDEXED, title, source)`); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO pages_fts (path, title, source) VALUES (?, ?, ?)`,
		"/legacy", "Legacy", "old body"); err != nil {
		t.Fatalf("legacy insert: %v", err)
	}

	s, err := NewSearchStore(db, nil)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if err := s.IndexPage("/new", "New", "new summary", []string{"x"}, "new body"); err != nil {
		t.Fatalf("insert after upgrade: %v", err)
	}
	hits, err := s.Query("new", nil, 10)
	if err != nil {
		t.Fatalf("query after upgrade: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit after upgrade, got %d", len(hits))
	}
}
