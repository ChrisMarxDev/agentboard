package auth

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// TestMigrateFreshInstall is the only migration test we keep pre-release —
// no upstream DBs exist yet, so the only path that runs in the wild is
// the fresh-install one. When we ship and start carrying real user data
// across version bumps, add a v1→v2 (etc) test here alongside the
// migration step.
func TestMigrateFreshInstall(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// `meta` is normally created by the data store; auth's migrate
	// writes its schema-version row into it.
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Schema accepts all three kinds.
	for _, k := range []string{"admin", "member", "bot"} {
		if _, err := db.Exec(
			`INSERT INTO users (username, kind, created_at) VALUES (?, ?, 1)`,
			k+"-user", k,
		); err != nil {
			t.Errorf("insert kind=%s: %v", k, err)
		}
	}
	// And the schema-version row was written.
	var v string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'auth_schema_version'`).Scan(&v); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if v != "2" {
		t.Errorf("schema_version = %q, want 2", v)
	}
}

// TestMigrateUpgradeFromV1 simulates the upstream upgrade case: a DB
// already populated by an older binary that wrote schema version 1,
// without the password columns or the user_sessions table. The
// migration must add them in place without losing data.
func TestMigrateUpgradeFromV1(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	// Hand-roll v1's user table without the v2 columns.
	if _, err := db.Exec(`
CREATE TABLE users (
    username       TEXT NOT NULL PRIMARY KEY COLLATE NOCASE,
    display_name   TEXT,
    kind           TEXT NOT NULL CHECK (kind IN ('admin','member','bot')),
    avatar_color   TEXT,
    access_mode    TEXT NOT NULL DEFAULT 'allow_all' CHECK (access_mode IN ('allow_all','restrict_to_list')),
    rules_json     TEXT NOT NULL DEFAULT '[]',
    created_at     INTEGER NOT NULL,
    created_by     TEXT,
    deactivated_at INTEGER
) STRICT;`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO users (username, kind, created_at) VALUES ('alice', 'admin', 1)`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO meta (key, value) VALUES ('auth_schema_version', '1')`,
	); err != nil {
		t.Fatal(err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("upgrade migrate: %v", err)
	}
	// Existing user is still there.
	var name string
	if err := db.QueryRow(`SELECT username FROM users WHERE username = 'alice'`).Scan(&name); err != nil {
		t.Fatalf("alice should still exist: %v", err)
	}
	// New columns are present.
	if !hasColumn(db, "users", "password_hash") || !hasColumn(db, "users", "password_updated_at") {
		t.Error("v2 password columns missing after upgrade")
	}
	// New table is present.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_sessions`).Scan(&n); err != nil {
		t.Errorf("user_sessions table should exist: %v", err)
	}
	// Schema version bumped.
	var v string
	_ = db.QueryRow(`SELECT value FROM meta WHERE key = 'auth_schema_version'`).Scan(&v)
	if v != "2" {
		t.Errorf("post-upgrade schema_version = %q, want 2", v)
	}
}
