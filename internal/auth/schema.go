// Package auth holds user and token management for AgentBoard.
//
// Invariants (see AUTH.md for the full rationale):
//
//   - username is the primary key of the users table. It's the identity —
//     `@alice` is the user `alice`, not a handle attached to some uuid.
//   - usernames are immutable via normal APIs. The CLI has a rename-user
//     escape hatch for the typo-on-create case; nothing else can rename.
//   - usernames are reserved forever. Deactivation sets deactivated_at but
//     the row stays; the PK uniqueness prevents re-creating `alice` later,
//     so every `@alice` reference anywhere keeps meaning the same person.
//   - a user owns zero or more tokens. Tokens have their own uuids because
//     they rotate and a user can hold several; the FK is users.username.
//   - kind = admin | agent. Admin tokens unlock /api/admin/*. Agent tokens
//     are scoped by the user's per-kind rules.
package auth

import (
	"database/sql"
	"fmt"
)

// schemaSQL is the full auth schema. Tracked under `auth_schema_version`.
//
// COLLATE NOCASE on users.username means "Alice" and "alice" collide for
// uniqueness even if some caller sidesteps the Go-side lowercasing.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS users (
    username       TEXT NOT NULL PRIMARY KEY COLLATE NOCASE,
    display_name   TEXT,
    kind           TEXT NOT NULL CHECK (kind IN ('admin','agent')),
    avatar_color   TEXT,
    access_mode    TEXT NOT NULL DEFAULT 'allow_all' CHECK (access_mode IN ('allow_all','restrict_to_list')),
    rules_json     TEXT NOT NULL DEFAULT '[]',
    created_at     INTEGER NOT NULL,
    created_by     TEXT,
    deactivated_at INTEGER
) STRICT;

CREATE TABLE IF NOT EXISTS user_tokens (
    id             TEXT NOT NULL PRIMARY KEY,
    username       TEXT NOT NULL REFERENCES users(username),
    token_hash     TEXT NOT NULL UNIQUE,
    label          TEXT,
    created_at     INTEGER NOT NULL,
    last_used_at   INTEGER,
    revoked_at     INTEGER
) STRICT;

CREATE INDEX IF NOT EXISTS idx_user_tokens_username ON user_tokens(username);
CREATE INDEX IF NOT EXISTS idx_user_tokens_active ON user_tokens(token_hash) WHERE revoked_at IS NULL;
`

func migrate(db *sql.DB) error {
	// Older drafts of the auth layer lived on auth_identities and an
	// id-keyed users/user_tokens shape. The user confirmed we don't have
	// any production data worth preserving, so the easiest reset is to
	// drop anything from those earlier shapes and rebuild.
	for _, t := range []string{"auth_sessions", "auth_bootstrap_codes", "auth_identities"} {
		_, _ = db.Exec(`DROP TABLE IF EXISTS ` + t)
	}

	// If a previous run created users/user_tokens under the older id-keyed
	// schema, drop them so the CREATE below produces the new shape.
	if legacy, err := hasLegacyUsersTable(db); err == nil && legacy {
		_, _ = db.Exec(`DROP TABLE IF EXISTS user_tokens`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS users`)
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("auth migrate: %w", err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('auth_schema_version', '4')`); err != nil {
		return fmt.Errorf("auth version: %w", err)
	}
	return nil
}

// hasLegacyUsersTable reports whether a `users` table exists that uses the
// old id-as-primary-key shape. The new shape has `username` as the PK and
// no `id` column at all.
func hasLegacyUsersTable(db *sql.DB) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	seenID := false
	seenUsername := false
	hasRow := false
	for rows.Next() {
		hasRow = true
		var (
			cid     int
			name    string
			typ     string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == "id" {
			seenID = true
		}
		if name == "username" {
			seenUsername = true
		}
	}
	if !hasRow {
		return false, nil // no users table at all
	}
	// Legacy if it has an id column. (New shape also has username but no id.)
	return seenID && seenUsername, nil
}
