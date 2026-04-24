package view

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// seedSession stands up an in-memory DB with share_tokens + view_sessions.
// share_tokens is minimal — we just need the FK target to exist.
func seedSession(t *testing.T) *SessionStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE share_tokens (id TEXT PRIMARY KEY) STRICT;
		INSERT INTO share_tokens VALUES ('tok-1');
		INSERT INTO share_tokens VALUES ('tok-2');
	`); err != nil {
		t.Fatal(err)
	}
	s, err := NewSessionStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSession_CreateResolve(t *testing.T) {
	s := seedSession(t)
	cookie, sess, err := s.Create("tok-1", "/handbook", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(cookie) == 0 || sess.Path != "/handbook" || sess.ShareTokenID != "tok-1" {
		t.Errorf("Create returned %q / %+v", cookie, sess)
	}
	got, err := s.Resolve(cookie)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ID != sess.ID || got.Path != "/handbook" {
		t.Errorf("Resolve returned %+v", got)
	}
}

func TestSession_DefaultTTL(t *testing.T) {
	s := seedSession(t)
	_, sess, _ := s.Create("tok-1", "/handbook", 0)
	if sess.ExpiresAt.Before(time.Now().UTC().Add(DefaultSessionTTL - time.Minute)) {
		t.Errorf("default TTL not applied: ExpiresAt=%v", sess.ExpiresAt)
	}
}

func TestSession_Expired(t *testing.T) {
	s := seedSession(t)
	cookie, _, _ := s.Create("tok-1", "/x", -time.Minute)
	_, err := s.Resolve(cookie)
	if err != ErrSessionExpired {
		t.Errorf("expected ErrSessionExpired, got %v", err)
	}
}

func TestSession_UnknownCookie(t *testing.T) {
	s := seedSession(t)
	if _, err := s.Resolve(""); err != ErrSessionNotFound {
		t.Errorf("empty cookie: %v", err)
	}
	if _, err := s.Resolve("not-real"); err != ErrSessionNotFound {
		t.Errorf("garbage cookie: %v", err)
	}
}

func TestSession_Delete(t *testing.T) {
	s := seedSession(t)
	cookie, sess, _ := s.Create("tok-1", "/p", time.Hour)
	if err := s.Delete(sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(cookie); err != ErrSessionNotFound {
		t.Errorf("post-delete: %v", err)
	}
}

func TestSession_DeleteByShareToken(t *testing.T) {
	s := seedSession(t)
	c1, _, _ := s.Create("tok-1", "/p", time.Hour)
	c2, _, _ := s.Create("tok-1", "/q", time.Hour)
	c3, _, _ := s.Create("tok-2", "/r", time.Hour)
	if err := s.DeleteByShareToken("tok-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(c1); err != ErrSessionNotFound {
		t.Errorf("tok-1 session 1 survived: %v", err)
	}
	if _, err := s.Resolve(c2); err != ErrSessionNotFound {
		t.Errorf("tok-1 session 2 survived: %v", err)
	}
	if _, err := s.Resolve(c3); err != nil {
		t.Errorf("tok-2 session wrongly deleted: %v", err)
	}
}

func TestSession_PurgeExpired(t *testing.T) {
	s := seedSession(t)
	c1, _, _ := s.Create("tok-1", "/p", -time.Minute)
	c2, _, _ := s.Create("tok-1", "/q", time.Hour)
	if err := s.PurgeExpired(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(c1); err != ErrSessionNotFound {
		t.Errorf("expired session survived purge: %v", err)
	}
	if _, err := s.Resolve(c2); err != nil {
		t.Errorf("valid session wrongly purged: %v", err)
	}
}
