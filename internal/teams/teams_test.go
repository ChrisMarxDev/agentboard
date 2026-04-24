package teams

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

func TestCreateAndGet(t *testing.T) {
	s, err := NewStore(openDB(t))
	if err != nil {
		t.Fatal(err)
	}
	team, err := s.Create(CreateParams{Slug: "marketing", DisplayName: "Marketing", CreatedBy: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if team.Slug != "marketing" {
		t.Errorf("slug = %q", team.Slug)
	}
	got, err := s.Get("Marketing") // case-insensitive lookup
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Marketing" {
		t.Errorf("display_name = %q", got.DisplayName)
	}
	if len(got.Members) != 0 {
		t.Errorf("expected empty members, got %d", len(got.Members))
	}
}

func TestReservedSlugs(t *testing.T) {
	s, _ := NewStore(openDB(t))
	for _, slug := range []string{"all", "admins", "agents", "here", "All", "HERE"} {
		if _, err := s.Create(CreateParams{Slug: slug}); err == nil {
			t.Errorf("expected reserved-slug rejection for %q", slug)
		}
	}
}

func TestInvalidSlugs(t *testing.T) {
	s, _ := NewStore(openDB(t))
	for _, slug := range []string{"1team", "-team", "has space", "", "Team!"} {
		if _, err := s.Create(CreateParams{Slug: slug}); err == nil {
			t.Errorf("expected invalid-slug rejection for %q", slug)
		}
	}
}

func TestUniqueSlug(t *testing.T) {
	s, _ := NewStore(openDB(t))
	if _, err := s.Create(CreateParams{Slug: "design"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(CreateParams{Slug: "design"}); err != ErrSlugTaken {
		t.Errorf("expected ErrSlugTaken, got %v", err)
	}
	// Case-insensitive collision.
	if _, err := s.Create(CreateParams{Slug: "DESIGN"}); err != ErrSlugTaken {
		t.Errorf("expected case-insensitive collision, got %v", err)
	}
}

func TestAddListRemoveMembers(t *testing.T) {
	s, _ := NewStore(openDB(t))
	if _, err := s.Create(CreateParams{Slug: "oncall"}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"alice", "bob", "charlie"} {
		if err := s.AddMember(AddMemberParams{Slug: "oncall", Username: u}); err != nil {
			t.Fatal(err)
		}
	}
	// Idempotent re-add (same slug+user) — no error.
	if err := s.AddMember(AddMemberParams{Slug: "oncall", Username: "alice", Role: "lead"}); err != nil {
		t.Fatalf("re-add failed: %v", err)
	}
	names, err := s.MemberUsernames("oncall")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Errorf("member count = %d, want 3", len(names))
	}
	// Role updated on re-insert.
	members, _ := s.ListMembers("oncall")
	var alice Member
	for _, m := range members {
		if m.Username == "alice" {
			alice = m
		}
	}
	if alice.Role != "lead" {
		t.Errorf("alice.role = %q, want lead", alice.Role)
	}

	if err := s.RemoveMember("oncall", "bob"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMember("oncall", "bob"); err != ErrNotMember {
		t.Errorf("second remove should be ErrNotMember, got %v", err)
	}
}

func TestAddMemberToMissingTeam(t *testing.T) {
	s, _ := NewStore(openDB(t))
	if err := s.AddMember(AddMemberParams{Slug: "ghost", Username: "alice"}); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestTeamsForUser(t *testing.T) {
	s, _ := NewStore(openDB(t))
	for _, slug := range []string{"design", "marketing", "oncall"} {
		if _, err := s.Create(CreateParams{Slug: slug}); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.AddMember(AddMemberParams{Slug: "design", Username: "alice"})
	_ = s.AddMember(AddMemberParams{Slug: "marketing", Username: "alice"})
	_ = s.AddMember(AddMemberParams{Slug: "oncall", Username: "bob"})

	teams, err := s.TeamsForUser("Alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 2 || teams[0] != "design" || teams[1] != "marketing" {
		t.Errorf("teams for alice = %v", teams)
	}
}

func TestDelete(t *testing.T) {
	s, _ := NewStore(openDB(t))
	if _, err := s.Create(CreateParams{Slug: "temp"}); err != nil {
		t.Fatal(err)
	}
	_ = s.AddMember(AddMemberParams{Slug: "temp", Username: "alice"})
	if err := s.Delete("temp"); err != nil {
		t.Fatal(err)
	}
	if s.Exists("temp") {
		t.Error("team still exists after delete")
	}
	// Member rows cascaded.
	teams, _ := s.TeamsForUser("alice")
	if len(teams) != 0 {
		t.Errorf("members not cleaned: %v", teams)
	}
	if err := s.Delete("temp"); err != ErrNotFound {
		t.Errorf("second delete: %v", err)
	}
}

func TestUpdate(t *testing.T) {
	s, _ := NewStore(openDB(t))
	if _, err := s.Create(CreateParams{Slug: "support"}); err != nil {
		t.Fatal(err)
	}
	dn := "Customer Support"
	if _, err := s.Update("support", UpdateParams{DisplayName: &dn}); err != nil {
		t.Fatal(err)
	}
	t2, _ := s.Get("support")
	if t2.DisplayName != "Customer Support" {
		t.Errorf("display_name = %q", t2.DisplayName)
	}
}

func TestValidateSlug(t *testing.T) {
	for _, tt := range []struct {
		slug string
		ok   bool
	}{
		{"marketing", true},
		{"m", true},
		{"team_1", true},
		{"team-1", true},
		{"Marketing", true}, // validates after lowercasing
		{"1team", false},
		{"-team", false},
		{"all", false},
		{"here", false},
		{"", false},
	} {
		err := ValidateSlug(tt.slug)
		if (err == nil) != tt.ok {
			t.Errorf("ValidateSlug(%q): ok=%v err=%v", tt.slug, tt.ok, err)
		}
	}
}
