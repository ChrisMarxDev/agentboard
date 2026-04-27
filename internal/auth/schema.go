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
// known starting point. The repo has no users in the wild yet, so the
// only path is fresh-install — `migrate()` does not branch on the
// stored value. Bump this and add the migration step alongside when
// that changes.
const schemaVersion = 1

// schemaSQL — current shape. Three kinds, token audit column, and the
// v0 mention/team/lock/invitation surface depend on this layout being
// stable.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS users (
    username       TEXT NOT NULL PRIMARY KEY COLLATE NOCASE,
    display_name   TEXT,
    kind           TEXT NOT NULL CHECK (kind IN ('admin','member','bot')),
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
    created_by     TEXT,
    last_used_at   INTEGER,
    revoked_at     INTEGER
) STRICT;

CREATE INDEX IF NOT EXISTS idx_user_tokens_username ON user_tokens(username);
CREATE INDEX IF NOT EXISTS idx_user_tokens_active ON user_tokens(token_hash) WHERE revoked_at IS NULL;
`

// migrate applies the schema. Pre-release: there are no upstream DBs
// to upgrade, so this is a single CREATE-IF-NOT-EXISTS pass plus a
// schema-version stamp. When we ship a real release, this is the file
// that grows version-gated branches.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("auth migrate: %w", err)
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
