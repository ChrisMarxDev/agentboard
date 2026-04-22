package auth

import (
	"database/sql"
	"errors"
	"log"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "auth.sqlite")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
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
	tok, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) < 20 || tok[:3] != "ab_" {
		t.Errorf("unexpected shape: %s", tok)
	}
	h := HashToken(tok)
	if h == tok {
		t.Error("hash should not equal plaintext")
	}
	if !TokensEqual(h, HashToken(tok)) {
		t.Error("stable hash lookup")
	}
}

func TestValidateUsername(t *testing.T) {
	good := []string{"alice", "a", "bot-1", "user_42", "a1"}
	for _, u := range good {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("ValidateUsername(%q) = %v", u, err)
		}
	}
	bad := []string{"", "1alice", "Alice", "a b", "a!", "a@b", "0abc", "🥜"}
	for _, u := range bad {
		if err := ValidateUsername(u); err == nil {
			t.Errorf("ValidateUsername(%q) expected error", u)
		}
	}
}

func TestUserTokenCRUD(t *testing.T) {
	s, _ := newTestStore(t)

	alice, err := s.CreateUser(CreateUserParams{Username: "alice", Kind: KindAgent})
	if err != nil {
		t.Fatal(err)
	}
	if alice.AvatarColor == "" {
		t.Error("expected avatar_color set on create")
	}

	// Case-insensitive duplicate.
	if _, err := s.CreateUser(CreateUserParams{Username: "ALICE", Kind: KindAgent}); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("case-insensitive dup = %v, want ErrUsernameTaken", err)
	}

	rawTok, _ := GenerateToken()
	tok, err := s.CreateToken(CreateTokenParams{
		Username: alice.Username, TokenHash: HashToken(rawTok), Label: "laptop",
	})
	if err != nil {
		t.Fatal(err)
	}

	user, resolved, err := s.ResolveToken(HashToken(rawTok))
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if user.Username != "alice" || resolved.ID != tok.ID {
		t.Errorf("mismatch: user=%s tok=%s", user.Username, resolved.ID)
	}

	// Rotate.
	newRaw, _ := GenerateToken()
	if err := s.RotateToken(tok.ID, HashToken(newRaw)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveToken(HashToken(rawTok)); !errors.Is(err, ErrNotFound) {
		t.Errorf("old token = %v, want ErrNotFound", err)
	}
	if _, _, err := s.ResolveToken(HashToken(newRaw)); err != nil {
		t.Errorf("new token unresolvable: %v", err)
	}

	// Second token coexists.
	secondRaw, _ := GenerateToken()
	if _, err := s.CreateToken(CreateTokenParams{
		Username: alice.Username, TokenHash: HashToken(secondRaw), Label: "ci",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeToken(tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveToken(HashToken(newRaw)); !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("revoked token = %v, want ErrTokenRevoked", err)
	}
	if _, _, err := s.ResolveToken(HashToken(secondRaw)); err != nil {
		t.Errorf("second token unresolvable: %v", err)
	}

	tokens, err := s.ListTokensForUser(alice.Username)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Errorf("want 2 tokens, got %d", len(tokens))
	}

	// Update mutable fields.
	newName := "Alice Chen"
	updated, err := s.UpdateUser(alice.Username, UpdateUserParams{DisplayName: &newName})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != "Alice Chen" {
		t.Errorf("display_name update: %q", updated.DisplayName)
	}

	// Deactivate — tokens revoked, subsequent resolves fail.
	if err := s.Deactivate(alice.Username); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveToken(HashToken(secondRaw)); !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("post-deactivate = %v, want ErrTokenRevoked", err)
	}

	// Username reserved forever — can't re-create.
	if _, err := s.CreateUser(CreateUserParams{Username: "alice", Kind: KindAgent}); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("re-create deactivated username = %v, want ErrUsernameTaken", err)
	}
}

func TestRenameUser(t *testing.T) {
	s, _ := newTestStore(t)
	u, _ := s.CreateUser(CreateUserParams{Username: "alicee", Kind: KindAgent})
	raw, _ := GenerateToken()
	tok, _ := s.CreateToken(CreateTokenParams{Username: u.Username, TokenHash: HashToken(raw)})

	stats, err := s.RenameUser("alicee", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if stats.UsersUpdated != 1 || stats.TokensUpdated != 1 {
		t.Errorf("stats: %+v", stats)
	}

	// Old username is gone.
	if _, err := s.GetUser("alicee"); !errors.Is(err, ErrNotFound) {
		t.Errorf("old username still resolves: %v", err)
	}
	// New username works.
	if renamed, err := s.GetUser("alice"); err != nil || renamed.Username != "alice" {
		t.Errorf("new username not set: %v %+v", err, renamed)
	}
	// Token row now references the new username.
	user, resolved, err := s.ResolveToken(HashToken(raw))
	if err != nil || user.Username != "alice" || resolved.ID != tok.ID {
		t.Errorf("token resolve after rename: user=%v tok=%v err=%v", user, resolved, err)
	}

	// Renaming to a taken username fails.
	if _, err := s.CreateUser(CreateUserParams{Username: "bob", Kind: KindAgent}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameUser("alice", "bob"); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("rename to taken = %v, want ErrUsernameTaken", err)
	}

	// Renaming to an invalid new name fails.
	if _, err := s.RenameUser("alice", "0bad"); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("rename to invalid = %v", err)
	}
}

func TestBootstrapOnEmpty(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.BootstrapOnEmpty("", log.New(&captureWriter{}, "", 0)); err != nil {
		t.Fatal(err)
	}
	admin, err := s.GetUser("admin")
	if err != nil {
		t.Fatalf("admin should exist: %v", err)
	}
	if admin.Kind != KindAdmin {
		t.Errorf("kind = %s", admin.Kind)
	}
	tokens, _ := s.ListTokensForUser(admin.Username)
	if len(tokens) != 1 {
		t.Errorf("want 1 initial token, got %d", len(tokens))
	}
	// Idempotent.
	before, _ := s.ListUsers(false)
	_ = s.BootstrapOnEmpty("", log.New(&captureWriter{}, "", 0))
	after, _ := s.ListUsers(false)
	if len(before) != len(after) {
		t.Errorf("bootstrap not idempotent")
	}
}

func TestBootstrapOnEmpty_WithLegacyToken(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.BootstrapOnEmpty("legacy-secret", log.New(&captureWriter{}, "", 0)); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.GetUser("legacy-agent")
	if err != nil {
		t.Fatalf("legacy-agent should exist: %v", err)
	}
	if legacy.Kind != KindAgent {
		t.Errorf("kind = %s", legacy.Kind)
	}
	if u, _, err := s.ResolveToken(HashToken("legacy-secret")); err != nil || u.Username != legacy.Username {
		t.Errorf("legacy token resolve: u=%v err=%v", u, err)
	}
	if _, err := s.GetUser("admin"); err != nil {
		t.Errorf("admin not created alongside legacy: %v", err)
	}
}

func TestResolveUsernames(t *testing.T) {
	s, _ := newTestStore(t)
	for _, name := range []string{"alice", "bob", "charlie"} {
		if _, err := s.CreateUser(CreateUserParams{Username: name, Kind: KindAgent}); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := s.ResolveUsernames([]string{"alice", "nobody", "CHARLIE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 2 {
		t.Errorf("want 2 matches, got %d", len(resolved))
	}
}

type captureWriter struct{ lines [][]byte }

func (c *captureWriter) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	copy(buf, p)
	c.lines = append(c.lines, buf)
	return len(p), nil
}
