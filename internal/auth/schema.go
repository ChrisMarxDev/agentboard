// Package auth holds user and token management for AgentBoard.
//
// Invariants (see AUTH.md for the full rationale):
//
//   - username is the primary key of the users table. It's the identity —
//     `@alice` IS the user `alice`, not a handle attached to some uuid.
//   - usernames are immutable via normal APIs. The CLI has a rename-user
//     escape hatch for the typo-on-create case; nothing else can rename.
//   - usernames are reserved forever. Deactivation sets deactivated_at but
//     the row stays; the PK uniqueness prevents re-creating `alice` later,
//     so every `@alice` reference anywhere keeps meaning the same person.
//   - a user owns zero or more tokens. Tokens have their own uuids because
//     they rotate and a user can hold several; the FK is users.username.
//   - kind = admin | member | bot. Admin unlocks /api/admin/*. Member is
//     a normal human user that can manage its own tokens. Bot is a shared
//     puppet — any admin can mint/rotate/revoke its tokens.
package auth

import (
	"database/sql"
	"fmt"
)

// schemaVersion is written into `meta` so a future migration has a
// known starting point. v2 adds password_hash + password_updated_at on
// users plus the user_sessions table — both gated on prior versions
// via ALTER TABLE / CREATE TABLE IF NOT EXISTS, so re-running on a
// fresh DB is also safe.
const schemaVersion = 2

// schemaSQL — current shape. Three kinds, token audit column, plus the
// password columns added in v2. user_sessions is its own table so a
// passkey login factor can plug in later without disturbing the
// password-only path.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS users (
    username             TEXT NOT NULL PRIMARY KEY COLLATE NOCASE,
    display_name         TEXT,
    kind                 TEXT NOT NULL CHECK (kind IN ('admin','member','bot')),
    avatar_color         TEXT,
    access_mode          TEXT NOT NULL DEFAULT 'allow_all' CHECK (access_mode IN ('allow_all','restrict_to_list')),
    rules_json           TEXT NOT NULL DEFAULT '[]',
    created_at           INTEGER NOT NULL,
    created_by           TEXT,
    deactivated_at       INTEGER,
    password_hash        TEXT,
    password_updated_at  INTEGER
) STRICT;

CREATE TABLE IF NOT EXISTS user_tokens (
    id             TEXT NOT NULL PRIMARY KEY,
    username       TEXT NOT NULL REFERENCES users(username),
    token_hash     TEXT NOT NULL UNIQUE,
    label          TEXT,
    created_at     INTEGER NOT NULL,
    created_by     TEXT,
    last_used_at   INTEGER,
    revoked_at     INTEGER
) STRICT;

CREATE INDEX IF NOT EXISTS idx_user_tokens_username ON user_tokens(username);
CREATE INDEX IF NOT EXISTS idx_user_tokens_active ON user_tokens(token_hash) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS user_sessions (
    id            TEXT NOT NULL PRIMARY KEY,
    session_hash  TEXT NOT NULL UNIQUE,
    username      TEXT NOT NULL REFERENCES users(username),
    created_at    INTEGER NOT NULL,
    last_used_at  INTEGER,
    expires_at    INTEGER NOT NULL,
    revoked_at    INTEGER,
    user_agent    TEXT,
    ip            TEXT
) STRICT;

CREATE INDEX IF NOT EXISTS idx_user_sessions_username ON user_sessions(username);
CREATE INDEX IF NOT EXISTS idx_user_sessions_active   ON user_sessions(session_hash) WHERE revoked_at IS NULL;
`

// migrate applies the schema. The current shape is created fresh via
// CREATE TABLE IF NOT EXISTS; any existing v1 database is upgraded
// in-place by adding the v2 password columns to users (the new
// user_sessions table is created by the IF-NOT-EXISTS path above).
//
// We branch on the previously-stored auth_schema_version. Anything
// less than 2 gets the password columns added.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("auth migrate: %w", err)
	}
	if _, err := db.Exec(oauthSchemaSQL); err != nil {
		return fmt.Errorf("auth migrate (oauth): %w", err)
	}

	// Check whether this DB was at v1 (no password_hash column on
	// users). On a fresh install schemaSQL above already created the
	// columns; on an upgrade we need to add them.
	if !hasColumn(db, "users", "password_hash") {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN password_hash TEXT`); err != nil {
			return fmt.Errorf("auth migrate v1->v2 (password_hash): %w", err)
		}
	}
	if !hasColumn(db, "users", "password_updated_at") {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN password_updated_at INTEGER`); err != nil {
			return fmt.Errorf("auth migrate v1->v2 (password_updated_at): %w", err)
		}
	}

	_, err := db.Exec(
		`INSERT OR REPLACE INTO meta (key, value) VALUES ('auth_schema_version', ?)`,
		fmt.Sprintf("%d", schemaVersion),
	)
	if err != nil {
		return fmt.Errorf("auth version: %w", err)
	}
	return nil
}

// hasColumn reports whether the given column already exists on the
// table. PRAGMA table_info returns one row per column; iterating over
// it is the only portable way SQLite gives us to introspect a schema.
func hasColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}
