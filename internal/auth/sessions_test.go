package auth

import (
	"errors"
	"testing"
	"time"
)

func TestSetPasswordAndVerifyLogin(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateUser(CreateUserParams{Username: "alice", Kind: KindMember}); err != nil {
		t.Fatal(err)
	}

	// No password yet → login fails.
	if _, err := s.VerifyLogin("alice", "anything-1234"); !errors.Is(err, ErrNotFound) {
		t.Errorf("login before SetPassword: err=%v want ErrNotFound", err)
	}

	if err := s.SetPassword("alice", "correct-password-1234"); err != nil {
		t.Fatal(err)
	}
	user, err := s.VerifyLogin("alice", "correct-password-1234")
	if err != nil {
		t.Fatalf("verify good password: %v", err)
	}
	if user.Username != "alice" {
		t.Errorf("user mismatch: %+v", user)
	}

	// Wrong password.
	if _, err := s.VerifyLogin("alice", "wrong-password-1234"); !errors.Is(err, ErrNotFound) {
		t.Errorf("wrong password: err=%v", err)
	}

	// Wrong username — same surface as wrong password (no info leak).
	if _, err := s.VerifyLogin("nobody", "anything-1234"); !errors.Is(err, ErrNotFound) {
		t.Errorf("wrong username: err=%v", err)
	}

	// Deactivated user.
	if err := s.Deactivate("alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.VerifyLogin("alice", "correct-password-1234"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deactivated user: err=%v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateUser(CreateUserParams{Username: "alice", Kind: KindMember}); err != nil {
		t.Fatal(err)
	}

	id, plain, err := s.CreateSession("alice", "Mozilla/test", "127.0.0.1", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || plain == "" {
		t.Fatal("CreateSession returned empty id or plaintext")
	}
	if !startsWith(plain, "abs_") {
		t.Errorf("plaintext should be abs_*, got %q", plain)
	}

	// Resolve the same plaintext.
	user, sess, err := s.ResolveSession(plain)
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	if user.Username != "alice" || sess.ID != id {
		t.Errorf("mismatch: user=%s sess=%s", user.Username, sess.ID)
	}
	if sess.UserAgent != "Mozilla/test" {
		t.Errorf("UA not stored: %q", sess.UserAgent)
	}

	// A bogus plaintext is invalid.
	if _, _, err := s.ResolveSession("abs_not-a-real-session-value"); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("bogus session: err=%v want ErrSessionInvalid", err)
	}

	// Revoke and confirm.
	if err := s.RevokeSession(id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveSession(plain); !errors.Is(err, ErrSessionRevoked) {
		t.Errorf("after revoke: err=%v want ErrSessionRevoked", err)
	}

	// Re-create + RevokeAllSessionsForUser.
	_, plain2, err := s.CreateSession("alice", "ua2", "::1", 0)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.RevokeAllSessionsForUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Errorf("expected at least 1 row revoked, got %d", n)
	}
	if _, _, err := s.ResolveSession(plain2); !errors.Is(err, ErrSessionRevoked) {
		t.Errorf("after revoke-all: err=%v", err)
	}
}

func TestSessionExpired(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateUser(CreateUserParams{Username: "alice", Kind: KindMember}); err != nil {
		t.Fatal(err)
	}
	id, plain, err := s.CreateSession("alice", "", "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Force expiry in the past.
	if _, err := s.db.Exec(`UPDATE user_sessions SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Minute).Unix(), id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveSession(plain); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("expired session: err=%v want ErrSessionExpired", err)
	}
}

func TestSessionUserDeactivated(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateUser(CreateUserParams{Username: "alice", Kind: KindMember}); err != nil {
		t.Fatal(err)
	}
	_, plain, _ := s.CreateSession("alice", "", "", 0)
	if err := s.Deactivate("alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveSession(plain); !errors.Is(err, ErrUserDeactivated) {
		t.Errorf("deactivated: err=%v", err)
	}
}

func TestListSessionsForUser(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateUser(CreateUserParams{Username: "alice", Kind: KindMember}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateSession("alice", "ua-a", "1.1.1.1", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateSession("alice", "ua-b", "2.2.2.2", 0); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSessionsForUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("got %d sessions, want 2", len(list))
	}
}

func TestGenerateCSRFToken(t *testing.T) {
	a, err := GenerateCSRFToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateCSRFToken()
	if a == b {
		t.Error("two CSRF tokens collided")
	}
	if !ConstantTimeStringEqual(a, a) {
		t.Error("self-equality failed")
	}
	if ConstantTimeStringEqual(a, b) {
		t.Error("distinct tokens compared equal")
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
