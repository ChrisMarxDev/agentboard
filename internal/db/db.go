// Package db opens the project's SQLite connection for the auth /
// teams / locks / invitations / mdx-meta / view-sessions / share /
// inbox / webhooks subsystems. The KV-data store has moved to files
// (internal/store), so SQLite is reduced to operational metadata.
//
// One connection pool per process. Auth + co-stores share it so we
// don't fight over the WAL file. Caller is responsible for Close on
// shutdown.
package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the underlying *sql.DB. The struct exists so we can
// evolve the open/close lifecycle without forcing every consumer to
// import database/sql directly — but right now Conn() is the only
// thing they need.
type DB struct {
	conn *sql.DB
}

// Open creates (or attaches to) the project SQLite file. The
// connection settings match what the legacy store used: WAL mode for
// concurrent readers + a 5 s busy-timeout so brief lock contention
// doesn't bubble up as errors.
func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", path, err)
	}
	// Probe the connection so misconfiguration surfaces here, not
	// later on a random query in another package.
	conn.SetConnMaxLifetime(0) // SQLite is in-process; no idle expiry.
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("db: ping %s: %w", path, err)
	}

	// Stamp creation time once, idempotent. Mirrors a behavior the
	// legacy store had — useful for support-bundle inspection.
	_, _ = conn.Exec(`CREATE TABLE IF NOT EXISTS meta (
		key   TEXT NOT NULL PRIMARY KEY,
		value TEXT NOT NULL
	) STRICT;`)
	_, _ = conn.Exec(
		`INSERT OR IGNORE INTO meta (key, value) VALUES ('created_at', ?)`,
		time.Now().UTC().Format(time.RFC3339),
	)

	return &DB{conn: conn}, nil
}

// Conn returns the underlying *sql.DB. Callers (auth, teams, etc.)
// pass it to their NewStore constructors. Don't close it directly —
// the *DB owns the lifecycle.
func (d *DB) Conn() *sql.DB { return d.conn }

// Close releases the connection pool. Safe to call multiple times.
func (d *DB) Close() error {
	if d.conn == nil {
		return nil
	}
	err := d.conn.Close()
	d.conn = nil
	return err
}
