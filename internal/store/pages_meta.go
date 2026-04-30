package store

import (
	"database/sql"
	"time"
)

// PageMeta is the per-page write metadata: who wrote it last, when.
// Intentionally minimal — when the full activity + history tables ship
// in v1, this collapses into a view over them. For the v0.5 dogfood cut
// this is the cheapest "last edited by" implementation that works for
// multi-user boards.
type PageMeta struct {
	LastActor string `json:"last_actor"`
	LastAt    string `json:"last_at"` // RFC3339Nano
}

// MetaStore persists one PageMeta row per page path.
type MetaStore struct {
	db *sql.DB
}

// NewMetaStore opens (and migrates) the page_meta table on the given
// database. Safe to call on every startup; the table creation is a no-op
// if it already exists.
func NewMetaStore(db *sql.DB) (*MetaStore, error) {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS page_meta (
		path       TEXT PRIMARY KEY,
		last_actor TEXT NOT NULL,
		last_at    TEXT NOT NULL
	) STRICT;
	`)
	if err != nil {
		return nil, err
	}
	return &MetaStore{db: db}, nil
}

// Record upserts the meta for a page. Actor should be the authenticated
// username when the caller knows it, else a free-form source (e.g. "mcp").
func (m *MetaStore) Record(path, actor string) error {
	if actor == "" {
		actor = "anonymous"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := m.db.Exec(
		`INSERT INTO page_meta (path, last_actor, last_at) VALUES (?, ?, ?)
		 ON CONFLICT (path) DO UPDATE SET last_actor = EXCLUDED.last_actor, last_at = EXCLUDED.last_at`,
		path, actor, now,
	)
	return err
}

// Get returns the meta for a page path, or nil when no write has been
// recorded (e.g. the seed pages that landed on disk before the store
// existed).
func (m *MetaStore) Get(path string) (*PageMeta, error) {
	var actor, at string
	err := m.db.QueryRow(
		`SELECT last_actor, last_at FROM page_meta WHERE path = ?`, path,
	).Scan(&actor, &at)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &PageMeta{LastActor: actor, LastAt: at}, nil
}

// Delete removes the meta for a page. Called from DeletePage handlers so a
// deleted page doesn't leave an orphan meta row around when it's later
// re-created by a different user.
func (m *MetaStore) Delete(path string) error {
	_, err := m.db.Exec(`DELETE FROM page_meta WHERE path = ?`, path)
	return err
}
