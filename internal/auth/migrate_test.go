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
	if v != "1" {
		t.Errorf("schema_version = %q, want 1", v)
	}
}
