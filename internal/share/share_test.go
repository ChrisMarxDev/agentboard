package share

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGenerate_FormatAndUniqueness(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Errorf("Generate produced two identical tokens")
	}
	if len(a) != len(TokenPrefix)+2*TokenRandBytes {
		t.Errorf("token length = %d, want %d", len(a), len(TokenPrefix)+2*TokenRandBytes)
	}
}

func TestStore_CreateResolveRevoke(t *testing.T) {
	db := openMemDB(t)
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, tok, err := s.Create(CreateParams{Path: "/handbook", CreatedBy: "alice", TTL: 0})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := s.Resolve(plaintext)
	if err != nil || resolved.ID != tok.ID {
		t.Fatalf("Resolve after Create: %v, tok=%+v", err, resolved)
	}
	if err := s.Revoke(tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(plaintext); err != ErrRevoked {
		t.Errorf("resolve after revoke = %v, want ErrRevoked", err)
	}
}

func TestStore_Expired(t *testing.T) {
	s, _ := NewStore(openMemDB(t))
	plaintext, _, err := s.Create(CreateParams{Path: "/x", CreatedBy: "alice", TTL: -time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(plaintext); err != ErrExpired {
		t.Errorf("resolve expired token = %v, want ErrExpired", err)
	}
}

func TestStore_ResolveUnknown(t *testing.T) {
	s, _ := NewStore(openMemDB(t))
	if _, err := s.Resolve("sh_notathing"); err != ErrNotFound {
		t.Errorf("resolve unknown = %v, want ErrNotFound", err)
	}
	if _, err := s.Resolve("ab_wrongprefix"); err != ErrNotFound {
		t.Errorf("resolve wrong-prefix = %v, want ErrNotFound", err)
	}
}

func TestStore_ListForPath(t *testing.T) {
	s, _ := NewStore(openMemDB(t))
	_, _, _ = s.Create(CreateParams{Path: "/p", CreatedBy: "alice", TTL: time.Hour})
	_, _, _ = s.Create(CreateParams{Path: "/p", CreatedBy: "bob", TTL: time.Hour})
	_, _, _ = s.Create(CreateParams{Path: "/other", CreatedBy: "alice", TTL: time.Hour})
	toks, err := s.ListForPath("/p")
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 2 {
		t.Errorf("ListForPath(/p) got %d tokens, want 2", len(toks))
	}
}
