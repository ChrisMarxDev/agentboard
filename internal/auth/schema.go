// Package auth holds the two-realm authentication primitives for AgentBoard:
// admin identities (session-cookie gated, browser-only) and agent identities
// (token gated, used by curl / Claude / MCP clients). See AUTH.md for the
// full design rationale.
//
// The package owns its own schema on the same SQLite file the data store
// uses — three tables all prefixed `auth_` so they never collide with the
// user-facing data keys.
package auth

import (
	"database/sql"
	"fmt"
)

// schemaSQL is the full auth schema. Safe to re-run; every DDL is CREATE IF
// NOT EXISTS. Schema version is tracked in the shared `meta` table under
// the key `auth_schema_version`.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS auth_identities (
    id              TEXT NOT NULL PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,
    kind            TEXT NOT NULL CHECK (kind IN ('admin','agent')),
    token_hash      TEXT,
    password_hash   TEXT,
    access_mode     TEXT NOT NULL DEFAULT 'allow_all' CHECK (access_mode IN ('allow_all','restrict_to_list')),
    rules_json      TEXT NOT NULL DEFAULT '[]',
    created_at      INTEGER NOT NULL,
    created_by      TEXT,
    last_used_at    INTEGER,
    revoked_at      INTEGER
) STRICT;

CREATE INDEX IF NOT EXISTS idx_auth_identities_token_hash ON auth_identities(token_hash)
    WHERE token_hash IS NOT NULL AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_auth_identities_name ON auth_identities(name);

CREATE TABLE IF NOT EXISTS auth_sessions (
    id              TEXT NOT NULL PRIMARY KEY,
    identity_id     TEXT NOT NULL REFERENCES auth_identities(id) ON DELETE CASCADE,
    csrf_token      TEXT NOT NULL,
    created_at      INTEGER NOT NULL,
    last_seen_at    INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL,
    user_agent      TEXT,
    ip              TEXT
) STRICT;

CREATE INDEX IF NOT EXISTS idx_auth_sessions_identity ON auth_sessions(identity_id);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires ON auth_sessions(expires_at);

CREATE TABLE IF NOT EXISTS auth_bootstrap_codes (
    id              TEXT NOT NULL PRIMARY KEY,
    code_hash       TEXT NOT NULL UNIQUE,
    created_at      INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL,
    used_at         INTEGER,
    note            TEXT
) STRICT;

CREATE INDEX IF NOT EXISTS idx_auth_bootstrap_expires ON auth_bootstrap_codes(expires_at);
`

// migrate applies schemaSQL and bumps the version tracker. Called by NewStore.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("auth migrate: %w", err)
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO meta (key, value) VALUES ('auth_schema_version', '1')`)
	if err != nil {
		return fmt.Errorf("auth version: %w", err)
	}
	return nil
}
