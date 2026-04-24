package inbox

import (
	"database/sql"
	"testing"
	"time"

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

func TestStore_CreateAndList(t *testing.T) {
	s, err := NewStore(openDB(t))
	if err != nil {
		t.Fatal(err)
	}
	for i, title := range []string{"hello", "bye"} {
		it, err := s.Create(CreateParams{
			Recipient: "alice",
			Kind:      KindMention,
			Title:     title,
			Actor:     "bob",
			SubjectRef: map[bool]string{true: "r1", false: "r2"}[i == 0],
		})
		if err != nil {
			t.Fatal(err)
		}
		if it.Recipient != "alice" || it.Actor != "bob" {
			t.Errorf("bad row: %+v", it)
		}
	}
	list, err := s.List(ListParams{Recipient: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("list len = %d", len(list))
	}
	// Newest first.
	if list[0].Title != "bye" {
		t.Errorf("sort order: %+v", list)
	}
}

func TestStore_Dedupe(t *testing.T) {
	s, _ := NewStore(openDB(t))
	a, _ := s.Create(CreateParams{Recipient: "alice", Kind: KindMention, Title: "ping", SubjectPath: "/x", SubjectRef: "anchor"})
	b, _ := s.Create(CreateParams{Recipient: "alice", Kind: KindMention, Title: "ping", SubjectPath: "/x", SubjectRef: "anchor"})
	if a.ID != b.ID {
		t.Errorf("dedupe failed: a=%d b=%d", a.ID, b.ID)
	}
}

func TestStore_UnreadCount(t *testing.T) {
	s, _ := NewStore(openDB(t))
	for i := 0; i < 3; i++ {
		_, _ = s.Create(CreateParams{
			Recipient: "alice", Kind: KindMention,
			Title: "n", SubjectRef: string(rune('a' + i)),
		})
	}
	n, _ := s.UnreadCount("alice")
	if n != 3 {
		t.Errorf("unread = %d, want 3", n)
	}
	// Mark one read, count drops.
	list, _ := s.List(ListParams{Recipient: "alice"})
	_ = s.MarkRead(list[0].ID, "alice")
	n, _ = s.UnreadCount("alice")
	if n != 2 {
		t.Errorf("unread after mark-read = %d, want 2", n)
	}
}

func TestStore_MarkAllRead(t *testing.T) {
	s, _ := NewStore(openDB(t))
	for i := 0; i < 4; i++ {
		_, _ = s.Create(CreateParams{
			Recipient: "alice", Kind: KindAssignment,
			Title: "t", SubjectRef: string(rune('A' + i)),
		})
	}
	n, err := s.MarkAllRead("alice")
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("marked %d, want 4", n)
	}
	cnt, _ := s.UnreadCount("alice")
	if cnt != 0 {
		t.Errorf("unread after mark-all-read: %d", cnt)
	}
}

func TestStore_ForbidOthers(t *testing.T) {
	s, _ := NewStore(openDB(t))
	it, _ := s.Create(CreateParams{Recipient: "alice", Kind: KindMention, Title: "hi"})
	if err := s.MarkRead(it.ID, "bob"); err != ErrForbidden {
		t.Errorf("bob marking alice's item: %v", err)
	}
	if err := s.Delete(it.ID, "bob"); err != ErrForbidden {
		t.Errorf("bob deleting: %v", err)
	}
	if err := s.Archive(it.ID, "bob"); err != ErrForbidden {
		t.Errorf("bob archiving: %v", err)
	}
}

func TestStore_ArchiveHidesFromList(t *testing.T) {
	s, _ := NewStore(openDB(t))
	it, _ := s.Create(CreateParams{Recipient: "alice", Kind: KindMention, Title: "hi"})
	_ = s.Archive(it.ID, "alice")
	list, _ := s.List(ListParams{Recipient: "alice"})
	if len(list) != 0 {
		t.Errorf("archived item still in list: %+v", list)
	}
}

func TestStore_PurgeOlderThan(t *testing.T) {
	s, _ := NewStore(openDB(t))
	_, _ = s.Create(CreateParams{Recipient: "alice", Kind: KindMention, Title: "fresh"})
	// Backdate one via raw insert.
	old := time.Now().UTC().Add(-90 * 24 * time.Hour).Unix()
	_, _ = s.db.Exec(`
		INSERT INTO inbox_items (recipient, kind, title, at) VALUES ('alice','mention','old',?)`, old)
	n, err := s.PurgeOlderThan(time.Now().UTC().Add(-60 * 24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("purged %d, want 1", n)
	}
	list, _ := s.List(ListParams{Recipient: "alice"})
	if len(list) != 1 || list[0].Title != "fresh" {
		t.Errorf("after purge: %+v", list)
	}
}
