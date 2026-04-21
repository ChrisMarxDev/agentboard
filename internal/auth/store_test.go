package auth

import (
	"database/sql"
	"errors"
	"log"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestStore opens a fresh in-memory-ish SQLite on disk and creates the
// meta table that auth.migrate() requires (normally created by the data
// store). Returning both the Store and raw DB lets individual tests inspect
// rows when needed.
func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "auth.sqlite")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// auth.migrate() writes to the shared `meta` table; create it.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return store, db
}

func TestTokenRoundTrip(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 20 || token[:3] != "ab_" {
		t.Errorf("unexpected token shape: %s", token)
	}
	hash := HashToken(token)
	if hash == token {
		t.Error("hash should not equal plaintext")
	}
	if !TokensEqual(hash, HashToken(token)) {
		t.Error("stable hash lookup")
	}
}

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword("correct horse battery staple", h); err != nil {
		t.Errorf("correct password should verify: %v", err)
	}
	if err := VerifyPassword("wrong", h); !errors.Is(err, ErrInvalidPassword) {
		t.Errorf("wrong password should return ErrInvalidPassword, got %v", err)
	}
}

func TestIdentityCRUD(t *testing.T) {
	s, _ := newTestStore(t)

	token, _ := GenerateToken()
	ident, err := s.CreateIdentity(CreateIdentityParams{
		Name:       "alice",
		Kind:       KindAgent,
		TokenHash:  HashToken(token),
		AccessMode: ModeAllowAll,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ident.ID == "" || ident.Name != "alice" || ident.Kind != KindAgent {
		t.Errorf("unexpected identity: %+v", ident)
	}

	// Lookup by token hash.
	found, err := s.GetIdentityByTokenHash(HashToken(token))
	if err != nil {
		t.Fatalf("lookup by token: %v", err)
	}
	if found.ID != ident.ID {
		t.Errorf("mismatch: %s != %s", found.ID, ident.ID)
	}

	// Name collision.
	_, err = s.CreateIdentity(CreateIdentityParams{
		Name:      "alice",
		Kind:      KindAgent,
		TokenHash: HashToken("x"),
	})
	if !errors.Is(err, ErrNameTaken) {
		t.Errorf("expected ErrNameTaken, got %v", err)
	}

	// Update rules + mode.
	newRules := []Rule{Allow("/api/data/marketing.**")}
	newMode := ModeRestrictToList
	updated, err := s.UpdateIdentity(ident.ID, UpdateIdentityParams{
		AccessMode: &newMode,
		Rules:      &newRules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.AccessMode != ModeRestrictToList || len(updated.Rules) != 1 {
		t.Errorf("update failed: %+v", updated)
	}

	// Revoke → lookup fails with ErrRevoked.
	if err := s.Revoke(ident.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.GetIdentityByTokenHash(HashToken(token))
	if !errors.Is(err, ErrRevoked) {
		t.Errorf("expected ErrRevoked after revoke, got %v", err)
	}

	// Revoke is idempotent-ish: second call returns ErrNotFound (no active row).
	if err := s.Revoke(ident.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound on re-revoke, got %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	s, _ := newTestStore(t)

	pw, _ := HashPassword("hunter2")
	admin, err := s.CreateIdentity(CreateIdentityParams{
		Name:         "admin",
		Kind:         KindAdmin,
		PasswordHash: pw,
	})
	if err != nil {
		t.Fatal(err)
	}

	sess, err := s.CreateSession(admin.ID, "ua", "1.2.3.4", 1*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == "" || sess.CSRFToken == "" {
		t.Error("session missing id or csrf token")
	}

	got, err := s.GetSession(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IdentityID != admin.ID {
		t.Errorf("identity mismatch: %s", got.IdentityID)
	}

	// Expired session.
	expired, err := s.CreateSession(admin.ID, "ua", "", -1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.GetSession(expired.ID)
	if !errors.Is(err, ErrSessionExpired) {
		t.Errorf("expected ErrSessionExpired, got %v", err)
	}

	// Delete all sessions for identity.
	if err := s.DeleteSessionsForIdentity(admin.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.GetSession(sess.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestBootstrapCodeOneTime(t *testing.T) {
	s, _ := newTestStore(t)

	code, _, err := s.CreateBootstrapCode(1*time.Hour, "test")
	if err != nil {
		t.Fatal(err)
	}

	// Wrong code.
	if err := s.ConsumeBootstrapCode("not-the-right-code"); !errors.Is(err, ErrCodeInvalid) {
		t.Errorf("expected ErrCodeInvalid for wrong code, got %v", err)
	}

	// Correct code consumes once.
	if err := s.ConsumeBootstrapCode(code); err != nil {
		t.Fatal(err)
	}

	// Second use fails.
	if err := s.ConsumeBootstrapCode(code); !errors.Is(err, ErrCodeInvalid) {
		t.Errorf("second use should fail, got %v", err)
	}
}

func TestBootstrapCodeExpires(t *testing.T) {
	s, _ := newTestStore(t)

	code, _, err := s.CreateBootstrapCode(-1*time.Second, "already-expired")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConsumeBootstrapCode(code); !errors.Is(err, ErrCodeInvalid) {
		t.Errorf("expired code should fail, got %v", err)
	}
}

func TestMigrateLegacyToken(t *testing.T) {
	s, _ := newTestStore(t)

	logger := log.New(&captureWriter{}, "", 0)
	if err := s.MigrateLegacyToken("old-secret-token", logger); err != nil {
		t.Fatal(err)
	}

	// Legacy agent should exist.
	ident, err := s.GetIdentityByName("legacy-agent")
	if err != nil {
		t.Fatalf("legacy-agent should exist: %v", err)
	}
	if ident.Kind != KindAgent {
		t.Errorf("kind = %s, want agent", ident.Kind)
	}
	if ident.TokenHash != HashToken("old-secret-token") {
		t.Error("legacy-agent token hash doesn't match")
	}

	// Second call is a no-op once any admin identity exists.
	// First, simulate admin creation via a bootstrap code consumption.
	_, err = s.CreateIdentity(CreateIdentityParams{
		Name:         "admin-alice",
		Kind:         KindAdmin,
		PasswordHash: mustHashPassword(t, "x"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Now migration is a no-op.
	before, _ := s.ListIdentities()
	if err := s.MigrateLegacyToken("old-secret-token", logger); err != nil {
		t.Fatal(err)
	}
	after, _ := s.ListIdentities()
	if len(before) != len(after) {
		t.Errorf("second migration should be no-op: before=%d after=%d", len(before), len(after))
	}
}

func mustHashPassword(t *testing.T, p string) string {
	t.Helper()
	h, err := HashPassword(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

type captureWriter struct{ lines [][]byte }

func (c *captureWriter) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	copy(buf, p)
	c.lines = append(c.lines, buf)
	return len(p), nil
}
