package webhooks

import (
	"database/sql"
	"testing"

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

func TestGenerateSecret_Format(t *testing.T) {
	a, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Errorf("Generate produced duplicates")
	}
	if len(a) != len(SecretPrefix)+2*SecretRandBytes {
		t.Errorf("unexpected length %d", len(a))
	}
}

func TestStore_CreateGetList(t *testing.T) {
	s, err := NewStore(openMemDB(t))
	if err != nil {
		t.Fatal(err)
	}
	secret, sub, err := s.Create(CreateParams{
		EventPattern:   "data.*",
		DestinationURL: "https://example.com/hook",
		Label:          "test",
		CreatedBy:      "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" || sub == nil || sub.EventPattern != "data.*" {
		t.Errorf("Create returned %q / %+v", secret, sub)
	}
	got, err := s.Get(sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DestinationURL != "https://example.com/hook" {
		t.Errorf("Get returned %+v", got)
	}
	list, err := s.ListActive()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("ListActive len = %d, want 1", len(list))
	}
}

func TestStore_Revoke(t *testing.T) {
	s, _ := NewStore(openMemDB(t))
	_, sub, _ := s.Create(CreateParams{
		EventPattern:   "*",
		DestinationURL: "https://x",
		CreatedBy:      "alice",
	})
	if err := s.Revoke(sub.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListActive()
	if len(list) != 0 {
		t.Errorf("revoked sub still in ListActive")
	}
	// Still visible in ListAll.
	all, _ := s.ListAll()
	if len(all) != 1 {
		t.Errorf("revoked sub missing from ListAll")
	}
	// Revoke missing id → ErrNotFound.
	if err := s.Revoke("nope"); err != ErrNotFound {
		t.Errorf("revoke missing: %v", err)
	}
}

func TestStore_UpdateAndRecordAttempt(t *testing.T) {
	s, _ := NewStore(openMemDB(t))
	_, sub, _ := s.Create(CreateParams{
		EventPattern:   "data.*",
		DestinationURL: "https://x",
		CreatedBy:      "alice",
	})
	newURL := "https://y"
	_, err := s.Update(sub.ID, UpdateParams{DestinationURL: &newURL})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(sub.ID)
	if got.DestinationURL != newURL {
		t.Errorf("Update didn't stick: %+v", got)
	}
	if err := s.RecordAttempt(sub.ID, StatusOK, 200, "", true); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(sub.ID)
	if got.SuccessCount != 1 || got.LastStatus != StatusOK {
		t.Errorf("RecordAttempt didn't stick: %+v", got)
	}
	if err := s.RecordAttempt(sub.ID, StatusRetrying, 500, "boom", false); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(sub.ID)
	if got.FailureCount != 1 || got.LastStatus != StatusRetrying || got.LastError != "boom" {
		t.Errorf("RecordAttempt fail case: %+v", got)
	}
}

func TestMatchEvent(t *testing.T) {
	cases := []struct {
		pattern, event string
		want           bool
	}{
		{"*", "anything", true},
		{"*", "", false},
		{"data", "data", true},
		{"data", "data.something", false},
		{"data.*", "data", false},
		{"data.*", "data.set", true},
		{"data.*", "data.set.welcome", true},
		{"data.set.*", "data.set.welcome", true},
		{"data.set.*", "data.merge.welcome", false},
		{"", "x", false},
		{"x", "", false},
		{"  data.*  ", "data.set", true},
	}
	for _, c := range cases {
		got := MatchEvent(c.pattern, c.event)
		if got != c.want {
			t.Errorf("MatchEvent(%q, %q) = %v, want %v", c.pattern, c.event, got, c.want)
		}
	}
}
