package auth

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// TestMigrateV4toV5 seeds a DB at schema v4 (with some rows in the old
// agent/admin shape), runs migrate, and asserts:
//   - agent rows became member; admin rows unchanged.
//   - the new CHECK constraint accepts inserts with kind='bot'.
//   - user_tokens.created_by column exists (new inserts can populate it).
//   - schema_version in meta is bumped to 5.
func TestMigrateV4toV5(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Seed the v4 shape manually — CREATE TABLE with the old CHECK.
	const v4Schema = `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);

CREATE TABLE users (
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

CREATE TABLE user_tokens (
    id             TEXT NOT NULL PRIMARY KEY,
    username       TEXT NOT NULL REFERENCES users(username),
    token_hash     TEXT NOT NULL UNIQUE,
    label          TEXT,
    created_at     INTEGER NOT NULL,
    last_used_at   INTEGER,
    revoked_at     INTEGER
) STRICT;

INSERT INTO meta (key, value) VALUES ('auth_schema_version', '4');
INSERT INTO users (username, display_name, kind, created_at) VALUES
    ('alice', 'Alice', 'agent', 100),
    ('bob',   'Bob',   'admin', 200),
    ('carol', 'Carol', 'agent', 300);
INSERT INTO user_tokens (id, username, token_hash, created_at) VALUES
    ('t1', 'alice', 'hash-a', 100),
    ('t2', 'bob',   'hash-b', 200);
`
	if _, err := db.Exec(v4Schema); err != nil {
		t.Fatal(err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Schema version bumped.
	v := readSchemaVersion(db)
	if v != 5 {
		t.Errorf("schema_version = %d, want 5", v)
	}

	// Agent rows became member.
	want := map[string]string{"alice": "member", "bob": "admin", "carol": "member"}
	for u, k := range want {
		var got string
		if err := db.QueryRow(`SELECT kind FROM users WHERE username = ?`, u).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", u, err)
		}
		if got != k {
			t.Errorf("kind(%s) = %q, want %q", u, got, k)
		}
	}

	// New CHECK constraint accepts bot.
	_, err = db.Exec(`INSERT INTO users (username, kind, created_at) VALUES ('helper', 'bot', 400)`)
	if err != nil {
		t.Errorf("insert bot-kind user: %v", err)
	}

	// CHECK constraint rejects the old 'agent' value.
	_, err = db.Exec(`INSERT INTO users (username, kind, created_at) VALUES ('foo', 'agent', 500)`)
	if err == nil {
		t.Error("expected CHECK constraint to reject kind='agent'")
	}

	// user_tokens.created_by column exists — insert with it.
	_, err = db.Exec(`INSERT INTO user_tokens (id, username, token_hash, created_at, created_by)
		VALUES ('t3', 'helper', 'hash-c', 400, 'bob')`)
	if err != nil {
		t.Errorf("insert with created_by: %v", err)
	}

	// Previous rows had NULL created_by — readable as empty string through scanner.
	var createdBy sql.NullString
	if err := db.QueryRow(`SELECT created_by FROM user_tokens WHERE id = 't1'`).Scan(&createdBy); err != nil {
		t.Fatal(err)
	}
	if createdBy.Valid {
		t.Errorf("pre-existing token's created_by should be NULL; got %q", createdBy.String)
	}

	// Idempotent — running migrate again is a no-op.
	if err := migrate(db); err != nil {
		t.Errorf("second migrate: %v", err)
	}
	if v := readSchemaVersion(db); v != 5 {
		t.Errorf("schema_version after re-migrate = %d, want 5", v)
	}
}

// TestMigrateFreshInstall exercises the "no meta, no users" branch:
// schemaV5SQL applied directly, schema_version written to 5.
func TestMigrateFreshInstall(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Seed meta table only — the data store normally owns this table
	// in production, so auth's migrate assumes it exists.
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if v := readSchemaVersion(db); v != 5 {
		t.Errorf("schema_version = %d, want 5", v)
	}
	// Fresh schema accepts all three kinds.
	for _, k := range []string{"admin", "member", "bot"} {
		if _, err := db.Exec(
			`INSERT INTO users (username, kind, created_at) VALUES (?, ?, 1)`,
			k+"-user", k,
		); err != nil {
			t.Errorf("insert kind=%s: %v", k, err)
		}
	}
}
