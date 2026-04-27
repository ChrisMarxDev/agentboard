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

// currentSchemaVersion tracks the incremental migration state. Bumped
// every time the auth schema changes in a way that requires a migration.
//
//	1-3: (pre-history — dropped via the legacy detector)
//	4:   users PK=username, kind IN ('admin','agent'), user_tokens FK
//	5:   kind rename agent→member, add 'bot'; user_tokens.created_by;
//	     new tables invitations + page_locks
const currentSchemaVersion = 5

// schemaV5SQL is the final shape: three kinds, token audit column,
// plus invitations and page_locks. Used by the fresh-install branch
// (schema_version absent / 0).
const schemaV5SQL = `
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

// migrate brings the auth schema to currentSchemaVersion. Composed of
// tiny version-gated steps so future bumps (v5→v6) add a single applyV6
// function alongside this one without rewriting what came before.
func migrate(db *sql.DB) error {
	// Legacy id-keyed users table — resurrect from prehistory by nuking
	// it so the vN CREATE below can lay down the new shape. Same posture
	// as before; all historical data is test data.
	for _, t := range []string{"auth_sessions", "auth_bootstrap_codes", "auth_identities"} {
		_, _ = db.Exec(`DROP TABLE IF EXISTS ` + t)
	}
	if legacy, err := hasLegacyUsersTable(db); err == nil && legacy {
		_, _ = db.Exec(`DROP TABLE IF EXISTS user_tokens`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS users`)
	}

	version := readSchemaVersion(db)
	if version >= currentSchemaVersion {
		return nil
	}

	// Fresh install — no rows to preserve. Apply v5 directly.
	if version == 0 {
		if _, err := db.Exec(schemaV5SQL); err != nil {
			return fmt.Errorf("auth migrate v5 (fresh): %w", err)
		}
		return writeSchemaVersion(db, currentSchemaVersion)
	}

	// v4 → v5 in-place upgrade (existing deployment with real user data).
	if version < 5 {
		if err := applyV5(db); err != nil {
			return fmt.Errorf("auth migrate v4→v5: %w", err)
		}
	}
	return writeSchemaVersion(db, currentSchemaVersion)
}

// applyV5 performs the v4→v5 migration in a single transaction:
//  1. Rebuild `users` with the new kind CHECK, mapping agent→member.
//  2. Add `user_tokens.created_by` column (nullable; existing rows get NULL).
//
// FK enforcement is disabled around the rebuild so the DROP/RENAME of
// `users` doesn't fail on user_tokens references; re-enabled + verified
// at the end. A non-empty `foreign_key_check` report aborts the migration
// because a partial v1 shape is worse than a failed boot.
func applyV5(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable FK: %w", err)
	}
	defer db.Exec(`PRAGMA foreign_keys = ON`)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Rebuild users with the new CHECK + agent→member backfill.
	if _, err := tx.Exec(`
		CREATE TABLE users_new (
		    username       TEXT NOT NULL PRIMARY KEY COLLATE NOCASE,
		    display_name   TEXT,
		    kind           TEXT NOT NULL CHECK (kind IN ('admin','member','bot')),
		    avatar_color   TEXT,
		    access_mode    TEXT NOT NULL DEFAULT 'allow_all' CHECK (access_mode IN ('allow_all','restrict_to_list')),
		    rules_json     TEXT NOT NULL DEFAULT '[]',
		    created_at     INTEGER NOT NULL,
		    created_by     TEXT,
		    deactivated_at INTEGER
		) STRICT`); err != nil {
		return fmt.Errorf("create users_new: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO users_new (username, display_name, kind, avatar_color, access_mode, rules_json, created_at, created_by, deactivated_at)
		SELECT username, display_name,
		       CASE kind WHEN 'agent' THEN 'member' ELSE kind END,
		       avatar_color, access_mode, rules_json, created_at, created_by, deactivated_at
		FROM users`); err != nil {
		return fmt.Errorf("copy users: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE users`); err != nil {
		return fmt.Errorf("drop users: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE users_new RENAME TO users`); err != nil {
		return fmt.Errorf("rename users_new: %w", err)
	}

	// 2. Add user_tokens.created_by. ALTER TABLE ADD COLUMN is a fast
	//    metadata-only change in SQLite and safe inside a transaction.
	if _, err := tx.Exec(`ALTER TABLE user_tokens ADD COLUMN created_by TEXT`); err != nil {
		// If the column somehow already exists (partial previous migration),
		// continue. SQLite reports "duplicate column name" on this path.
		if !isDuplicateColumnErr(err) {
			return fmt.Errorf("add user_tokens.created_by: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Post-commit FK integrity check. Any violation is fatal.
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("foreign_key_check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("foreign_key_check reported violations after v5 migration; aborting")
	}
	return nil
}

func readSchemaVersion(db *sql.DB) int {
	var v sql.NullString
	_ = db.QueryRow(`SELECT value FROM meta WHERE key = 'auth_schema_version'`).Scan(&v)
	if !v.Valid {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(v.String, "%d", &n)
	return n
}

func writeSchemaVersion(db *sql.DB, version int) error {
	_, err := db.Exec(
		`INSERT OR REPLACE INTO meta (key, value) VALUES ('auth_schema_version', ?)`,
		fmt.Sprintf("%d", version),
	)
	if err != nil {
		return fmt.Errorf("auth version: %w", err)
	}
	return nil
}

// hasLegacyUsersTable reports whether a `users` table exists that uses
// the pre-v4 id-as-primary-key shape. The v4+ shape has `username` as
// the PK and no `id` column at all.
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
		return false, nil
	}
	return seenID && seenUsername, nil
}

func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	return contains(m, "duplicate column name") || contains(m, "already exists")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
