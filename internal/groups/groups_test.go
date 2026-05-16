package groups

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

// newTestStore opens an in-memory SQLite with FKs ON and a minimal
// `users` table so the group_members FK has a target. The auth
// package's full schema isn't needed for groups-level tests.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE users (
		username TEXT PRIMARY KEY,
		kind     TEXT NOT NULL DEFAULT 'member'
	) STRICT;`); err != nil {
		t.Fatalf("create users: %v", err)
	}
	s, err := NewStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s
}

func seedUsers(t *testing.T, s *Store, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := s.db.Exec(`INSERT INTO users(username) VALUES (?)`, n); err != nil {
			t.Fatalf("seed user %s: %v", n, err)
		}
	}
}

func TestValidateName(t *testing.T) {
	// Accepted: includes "UPPER" — auto-lowercases per package contract.
	good := []string{"marketing", "eng", "team-1", "a", "z9", "with_underscores", "UPPER", "Marketing"}
	for _, n := range good {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) want nil, got %v", n, err)
		}
	}
	bad := []string{"", "1leadingdigit", "has space", "has.dot", "this-name-is-way-too-long-to-fit-in-the-limit", "admins", "Admins"}
	for _, n := range bad {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) want error, got nil", n)
		}
	}
}

func TestCreateAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	g, err := s.Create(ctx, "marketing", "alice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if g.Name != "marketing" || g.CreatedBy != "alice" || g.ID == "" {
		t.Fatalf("unexpected created group: %+v", g)
	}

	got, err := s.Get(ctx, "Marketing") // case-insensitive lookup
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != g.ID {
		t.Errorf("get returned different id: %s vs %s", got.ID, g.ID)
	}

	if _, err := s.Get(ctx, "nonexistent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing group: want ErrNotFound, got %v", err)
	}
}

func TestCreateDuplicateName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Create(ctx, "team", "alice"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := s.Create(ctx, "team", "bob"); !errors.Is(err, ErrNameTaken) {
		t.Errorf("duplicate: want ErrNameTaken, got %v", err)
	}
	// Case-insensitive collision.
	if _, err := s.Create(ctx, "TEAM", "bob"); !errors.Is(err, ErrNameTaken) {
		t.Errorf("case-insensitive dup: want ErrNameTaken, got %v", err)
	}
}

func TestCreateRejectsReserved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Create(ctx, "admins", "alice"); !errors.Is(err, ErrInvalidName) {
		t.Errorf("create admins: want ErrInvalidName, got %v", err)
	}
}

func TestAddRemoveMember(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedUsers(t, s, "alice", "bob", "carol")

	if _, err := s.Create(ctx, "marketing", "alice"); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, u := range []string{"alice", "bob", "carol"} {
		if err := s.AddMember(ctx, "marketing", u); err != nil {
			t.Fatalf("add %s: %v", u, err)
		}
	}

	members, err := s.Members(ctx, "marketing")
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}

	// Idempotent re-add.
	if err := s.AddMember(ctx, "marketing", "alice"); err != nil {
		t.Errorf("re-add: want nil, got %v", err)
	}
	if members2, _ := s.Members(ctx, "marketing"); len(members2) != 3 {
		t.Errorf("after re-add still 3 members, got %d", len(members2))
	}

	if err := s.RemoveMember(ctx, "marketing", "bob"); err != nil {
		t.Errorf("remove bob: %v", err)
	}
	if err := s.RemoveMember(ctx, "marketing", "bob"); !errors.Is(err, ErrNotMember) {
		t.Errorf("remove bob twice: want ErrNotMember, got %v", err)
	}
}

func TestAddMemberMissingUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Create(ctx, "team", "alice"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AddMember(ctx, "team", "ghost"); !errors.Is(err, ErrUserUnknown) {
		t.Errorf("add ghost: want ErrUserUnknown, got %v", err)
	}
}

func TestAddMemberMissingGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedUsers(t, s, "alice")

	if err := s.AddMember(ctx, "nogroup", "alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("add to missing group: want ErrNotFound, got %v", err)
	}
}

func TestDeleteGroupCascadesMembers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedUsers(t, s, "alice", "bob")

	if _, err := s.Create(ctx, "team", "alice"); err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = s.AddMember(ctx, "team", "alice")
	_ = s.AddMember(ctx, "team", "bob")

	if err := s.Delete(ctx, "team"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// FK cascade should have wiped the membership rows.
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM group_members`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 group_members after delete, got %d", n)
	}

	if err := s.Delete(ctx, "team"); !errors.Is(err, ErrNotFound) {
		t.Errorf("double-delete: want ErrNotFound, got %v", err)
	}
}

func TestUserDeleteCascadesMembership(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedUsers(t, s, "alice", "bob")

	if _, err := s.Create(ctx, "team", "alice"); err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = s.AddMember(ctx, "team", "alice")
	_ = s.AddMember(ctx, "team", "bob")

	// Hard-delete a user. (Production uses soft-delete via deactivated_at,
	// but the FK contract should still hold for either path.)
	if _, err := s.db.Exec(`DELETE FROM users WHERE username = ?`, "bob"); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	members, _ := s.Members(ctx, "team")
	if len(members) != 1 || members[0].Username != "alice" {
		t.Errorf("after user delete: expected only alice, got %+v", members)
	}
}

func TestRename(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedUsers(t, s, "alice")

	g, _ := s.Create(ctx, "old", "alice")
	originalID := g.ID

	if err := s.Rename(ctx, "old", "new"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, err := s.Get(ctx, "new")
	if err != nil {
		t.Fatalf("get after rename: %v", err)
	}
	if got.ID != originalID {
		t.Errorf("rename changed id: %s → %s", originalID, got.ID)
	}
	if _, err := s.Get(ctx, "old"); !errors.Is(err, ErrNotFound) {
		t.Errorf("old name still resolves: %v", err)
	}

	// Rename to a reserved word fails.
	if err := s.Rename(ctx, "new", "admins"); !errors.Is(err, ErrInvalidName) {
		t.Errorf("rename to admins: want ErrInvalidName, got %v", err)
	}

	// Rename a missing group fails.
	if err := s.Rename(ctx, "ghost", "spectre"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rename missing: want ErrNotFound, got %v", err)
	}

	// Collision rename fails.
	_, _ = s.Create(ctx, "other", "alice")
	if err := s.Rename(ctx, "new", "other"); !errors.Is(err, ErrNameTaken) {
		t.Errorf("collision rename: want ErrNameTaken, got %v", err)
	}
}

func TestMemberOfAndList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedUsers(t, s, "alice", "bob")

	_, _ = s.Create(ctx, "marketing", "alice")
	_, _ = s.Create(ctx, "engineering", "alice")
	_, _ = s.Create(ctx, "ops", "alice")

	_ = s.AddMember(ctx, "marketing", "alice")
	_ = s.AddMember(ctx, "engineering", "alice")
	_ = s.AddMember(ctx, "marketing", "bob")

	groups, err := s.MemberOf(ctx, "alice")
	if err != nil {
		t.Fatalf("member-of alice: %v", err)
	}
	if len(groups) != 2 {
		t.Errorf("alice in 2 groups, got %d (%v)", len(groups), groups)
	}

	all, err := s.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("list: want 3, got %d", len(all))
	}
	// Ordered by name.
	wantOrder := []string{"engineering", "marketing", "ops"}
	for i, g := range all {
		if g.Name != wantOrder[i] {
			t.Errorf("list[%d]: want %s, got %s", i, wantOrder[i], g.Name)
		}
	}
}
