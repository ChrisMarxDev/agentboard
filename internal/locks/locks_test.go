package locks

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestLockUnlockRoundTrip(t *testing.T) {
	s, _ := NewStore(openDB(t))
	l, err := s.Lock(LockParams{Path: "/handbook", LockedBy: "alice", Reason: "canonical"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != "handbook" { // leading slash normalized away
		t.Errorf("path = %q", l.Path)
	}
	if !s.IsLocked("/handbook") || !s.IsLocked("handbook") {
		t.Error("IsLocked should be true")
	}
	got, _ := s.Get("handbook")
	if got.LockedBy != "alice" || got.Reason != "canonical" {
		t.Errorf("get mismatch: %+v", got)
	}
	if err := s.Unlock("/handbook"); err != nil {
		t.Fatal(err)
	}
	if s.IsLocked("handbook") {
		t.Error("should be unlocked")
	}
}

func TestReLock(t *testing.T) {
	s, _ := NewStore(openDB(t))
	_, _ = s.Lock(LockParams{Path: "notes", LockedBy: "alice", Reason: "v1"})
	_, _ = s.Lock(LockParams{Path: "notes", LockedBy: "bob", Reason: "v2"})
	got, _ := s.Get("notes")
	if got.LockedBy != "bob" || got.Reason != "v2" {
		t.Errorf("re-lock should overwrite: %+v", got)
	}
}

func TestUnlockMissing(t *testing.T) {
	s, _ := NewStore(openDB(t))
	if err := s.Unlock("ghost"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRename(t *testing.T) {
	s, _ := NewStore(openDB(t))
	_, _ = s.Lock(LockParams{Path: "old-path", LockedBy: "alice"})
	if err := s.Rename("old-path", "new-path"); err != nil {
		t.Fatal(err)
	}
	if s.IsLocked("old-path") {
		t.Error("old path still locked")
	}
	if !s.IsLocked("new-path") {
		t.Error("new path should be locked")
	}
	// Rename on a non-locked path is a no-op (not an error).
	if err := s.Rename("nothing-here", "also-nothing"); err != nil {
		t.Errorf("rename on missing path: %v", err)
	}
}

func TestList(t *testing.T) {
	s, _ := NewStore(openDB(t))
	_, _ = s.Lock(LockParams{Path: "a", LockedBy: "alice"})
	_, _ = s.Lock(LockParams{Path: "b", LockedBy: "bob"})
	ls, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 2 {
		t.Errorf("List len = %d", len(ls))
	}
}

func TestNormalize(t *testing.T) {
	for _, tt := range []struct{ in, out string }{
		{"/handbook", "handbook"},
		{"handbook.md", "handbook"},
		{"/handbook.mdx", "handbook"},
		{"  /foo/bar  ", "foo/bar"},
		{"", ""},
	} {
		if got := normalize(tt.in); got != tt.out {
			t.Errorf("normalize(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}
