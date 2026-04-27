package auth

import (
	"database/sql"
	"errors"
	"log"
	"path/filepath"
	"testing"

	"github.com/christophermarx/agentboard/internal/invitations"
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

	alice, err := s.CreateUser(CreateUserParams{Username: "alice", Kind: KindMember})
	if err != nil {
		t.Fatal(err)
	}
	if alice.AvatarColor == "" {
		t.Error("expected avatar_color set on create")
	}

	// Case-insensitive duplicate.
	if _, err := s.CreateUser(CreateUserParams{Username: "ALICE", Kind: KindMember}); !errors.Is(err, ErrUsernameTaken) {
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
	if _, err := s.CreateUser(CreateUserParams{Username: "alice", Kind: KindMember}); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("re-create deactivated username = %v, want ErrUsernameTaken", err)
	}
}

func TestRenameUser(t *testing.T) {
	s, _ := newTestStore(t)
	u, _ := s.CreateUser(CreateUserParams{Username: "alicee", Kind: KindMember})
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
	if _, err := s.CreateUser(CreateUserParams{Username: "bob", Kind: KindMember}); err != nil {
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

func TestBootstrapFirstAdmin_MintsInvite(t *testing.T) {
	// With no legacy token and an empty DB, BootstrapFirstAdmin mints
	// a role=admin invitation so the operator can claim the first
	// admin via /invite/<id>.
	s, db := newTestStore(t)
	invStore, err := invitations.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := s.BootstrapFirstAdmin(invStore, "", 0, log.New(&captureWriter{}, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if inv == nil || inv.Role != invitations.RoleAdmin {
		t.Errorf("expected admin invite, got %+v", inv)
	}
	if inv.CreatedBy != invitations.BootstrapCreator {
		t.Errorf("created_by = %q", inv.CreatedBy)
	}
	users, _ := s.ListUsers(false)
	if len(users) != 0 {
		t.Errorf("no users should exist yet, got %d", len(users))
	}
}

func TestBootstrapFirstAdmin_Idempotent(t *testing.T) {
	s, db := newTestStore(t)
	invStore, _ := invitations.NewStore(db)
	first, err := s.BootstrapFirstAdmin(invStore, "", 0, nil)
	if err != nil || first == nil {
		t.Fatal(err)
	}
	second, err := s.BootstrapFirstAdmin(invStore, "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Errorf("second bootstrap should reuse: first=%q second=%q", first.ID, second.ID)
	}
}

func TestBootstrapFirstAdmin_WithLegacyToken(t *testing.T) {
	s, db := newTestStore(t)
	invStore, _ := invitations.NewStore(db)
	// Legacy token path short-circuits — seeds @legacy-agent and
	// skips the bootstrap invite (an identity already exists).
	inv, err := s.BootstrapFirstAdmin(invStore, "legacy-secret", 0, log.New(&captureWriter{}, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if inv != nil {
		t.Errorf("expected nil invite with legacy token; got %+v", inv)
	}
	legacy, err := s.GetUser("legacy-agent")
	if err != nil {
		t.Fatalf("legacy-agent should exist: %v", err)
	}
	if legacy.Kind != KindMember {
		t.Errorf("kind = %s", legacy.Kind)
	}
	if u, _, err := s.ResolveToken(HashToken("legacy-secret")); err != nil || u.Username != legacy.Username {
		t.Errorf("legacy token resolve: u=%v err=%v", u, err)
	}
}

func TestBootstrapFirstAdmin_ExistingUserSkips(t *testing.T) {
	s, db := newTestStore(t)
	invStore, _ := invitations.NewStore(db)
	// Seed a user first.
	if _, err := s.CreateUser(CreateUserParams{Username: "alice", Kind: KindAdmin}); err != nil {
		t.Fatal(err)
	}
	inv, err := s.BootstrapFirstAdmin(invStore, "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if inv != nil {
		t.Errorf("bootstrap should be no-op when users exist; got %+v", inv)
	}
}

func TestResolveUsernames(t *testing.T) {
	s, _ := newTestStore(t)
	for _, name := range []string{"alice", "bob", "charlie"} {
		if _, err := s.CreateUser(CreateUserParams{Username: name, Kind: KindMember}); err != nil {
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
