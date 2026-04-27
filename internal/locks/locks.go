// Package locks implements per-page write locks. A locked page renders
// and lists normally but rejects PUT/DELETE from non-admin bearers.
// Distinct from the existing `page_approvals` primitive, which records
// "this version was reviewed" without restricting future edits.
//
// Storage is a single-table key-value row per path. Index-only lookup
// on the hot IsLocked() path: `SELECT 1 FROM page_locks WHERE path=?`
// backed by the PK. No secondary indexes needed.
package locks

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Lock is one row.
type Lock struct {
	Path     string    `json:"path"`
	LockedBy string    `json:"locked_by"`
	LockedAt time.Time `json:"locked_at"`
	Reason   string    `json:"reason,omitempty"`
}

// Standard errors.
var (
	ErrNotFound = errors.New("locks: not found")
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS page_locks (
    path       TEXT PRIMARY KEY,
    locked_by  TEXT NOT NULL,
    locked_at  INTEGER NOT NULL,
    reason     TEXT
) STRICT;
`

// Store owns the page_locks table.
type Store struct {
	db *sql.DB
}

// NewStore migrates and returns a store.
func NewStore(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("locks: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// LockParams bundles inputs for Lock.
type LockParams struct {
	Path     string
	LockedBy string
	Reason   string
}

// Lock records a lock for path. If the path is already locked,
// overwrites the existing row (new locker / new reason — the intent
// is "this page is admin-only", not "this particular admin owns it").
func (s *Store) Lock(p LockParams) (*Lock, error) {
	p.Path = normalize(p.Path)
	if p.Path == "" {
		return nil, fmt.Errorf("locks: path required")
	}
	if p.LockedBy == "" {
		return nil, fmt.Errorf("locks: locked_by required")
	}
	now := time.Now().UTC().Unix()
	_, err := s.db.Exec(
		`INSERT INTO page_locks (path, locked_by, locked_at, reason)
		 VALUES (?, ?, ?, NULLIF(?, ''))
		 ON CONFLICT(path) DO UPDATE SET
		   locked_by = excluded.locked_by,
		   locked_at = excluded.locked_at,
		   reason    = excluded.reason`,
		p.Path, p.LockedBy, now, p.Reason,
	)
	if err != nil {
		return nil, fmt.Errorf("locks: insert: %w", err)
	}
	return s.Get(p.Path)
}

// Unlock removes the lock row. Returns ErrNotFound if none exists.
func (s *Store) Unlock(path string) error {
	path = normalize(path)
	res, err := s.db.Exec(`DELETE FROM page_locks WHERE path = ?`, path)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Get returns the lock for a path, or ErrNotFound.
func (s *Store) Get(path string) (*Lock, error) {
	path = normalize(path)
	row := s.db.QueryRow(
		`SELECT path, locked_by, locked_at, reason FROM page_locks WHERE path = ?`, path)
	l, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return l, err
}

// IsLocked is the cheap hot-path check called on every page write.
// Returns false on any DB error — write-path failures dominate over
// accidentally-allowing-an-edit, and the edit gate is defence in
// depth (the admin gate is the primary authority).
func (s *Store) IsLocked(path string) bool {
	path = normalize(path)
	if path == "" {
		return false
	}
	var x string
	err := s.db.QueryRow(`SELECT path FROM page_locks WHERE path = ?`, path).Scan(&x)
	return err == nil
}

// List returns every lock, newest first.
func (s *Store) List() ([]*Lock, error) {
	rows, err := s.db.Query(
		`SELECT path, locked_by, locked_at, reason FROM page_locks ORDER BY locked_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Lock
	for rows.Next() {
		l, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Rename moves the lock from one path to another. Called by the page
// move handler after a successful file rename so the lock travels
// with the path. Idempotent: no-op if nothing was locked at `from`.
func (s *Store) Rename(from, to string) error {
	from = normalize(from)
	to = normalize(to)
	if from == to {
		return nil
	}
	_, err := s.db.Exec(`UPDATE page_locks SET path = ? WHERE path = ?`, to, from)
	return err
}

// -------- helpers --------

type rowScanner interface {
	Scan(dest ...any) error
}

func scan(r rowScanner) (*Lock, error) {
	var (
		l        Lock
		lockedAt int64
		reason   sql.NullString
	)
	if err := r.Scan(&l.Path, &l.LockedBy, &lockedAt, &reason); err != nil {
		return nil, err
	}
	l.LockedAt = time.Unix(lockedAt, 0).UTC()
	if reason.Valid {
		l.Reason = reason.String
	}
	return &l, nil
}

// normalize makes `/handbook`, `handbook`, and `handbook.md` resolve
// to the same row. Matches the convention used by approvals + page
// refs — canonical form is the leading-slashless path, no extension.
func normalize(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, ".md")
	p = strings.TrimSuffix(p, ".mdx")
	return p
}
