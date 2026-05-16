// Package groups stores named collections of users referenced by
// the permission system. Groups are flat — no nesting, no roles
// within a group, no expiry.
//
// Identity model:
//
//   - PK is an opaque ULID-shaped string. Names are mutable; the ID
//     stays put across renames so the permission rules that reference
//     a group by id never break. (Permission rules in YAML reference
//     by name for human readability; the resolver translates name →
//     id at evaluation time.)
//   - Membership references `users(username)` directly since usernames
//     are the auth-side primary key and they're immutable.
//   - The synthetic group `admins` is RESERVED: callers can't create
//     a group with that name. Permission rules that say `write:
//     [admins]` resolve through `user.Kind == admin`, not through this
//     table.
package groups

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Standard errors. Handlers map these to HTTP status codes.
var (
	ErrNotFound    = errors.New("groups: not found")
	ErrNameTaken   = errors.New("groups: name already taken")
	ErrInvalidName = errors.New("groups: name must be 1-32 lowercase letters, digits, dashes, or underscores; cannot start with a digit; cannot be `admins`")
	ErrUserUnknown = errors.New("groups: user does not exist")
	ErrNotMember   = errors.New("groups: user is not a member")
)

// ReservedNames are forbidden as group names. The permission resolver
// treats these as synthetic groups derived from user role / state.
var ReservedNames = map[string]struct{}{
	"admins": {},
}

// nameRe is the validation pattern for group names. Matches our
// conventional namespace: lowercase ASCII, starts with a letter, 1-32
// chars, allowing dashes and underscores after the first char.
var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Group is one row in the groups table.
type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// Member is one row in the group_members join table. Username
// references users(username) — see package doc.
type Member struct {
	Username string    `json:"username"`
	AddedAt  time.Time `json:"added_at"`
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS groups (
    id          TEXT NOT NULL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
    created_at  INTEGER NOT NULL,
    created_by  TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS group_members (
    group_id    TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    username    TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    added_at    INTEGER NOT NULL,
    PRIMARY KEY (group_id, username)
) STRICT;

CREATE INDEX IF NOT EXISTS idx_group_members_username ON group_members(username);
`

// Store is the persistence handle. NewStore runs the (idempotent)
// migration; callers reuse the returned *Store across requests.
type Store struct {
	db *sql.DB
}

// NewStore opens the groups schema against conn. Idempotent: safe to
// call on an existing or fresh DB.
func NewStore(conn *sql.DB) (*Store, error) {
	if conn == nil {
		return nil, errors.New("groups: nil connection")
	}
	if _, err := conn.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("groups: migrate: %w", err)
	}
	return &Store{db: conn}, nil
}

// ValidateName checks the convention without touching the DB. Useful
// in CLI / UI for early feedback. Returns ErrInvalidName when bad.
// Input is auto-lowercased — "Marketing" and "marketing" are the
// same group for non-technical users who don't want to think about
// case. The store always persists lowercase.
func ValidateName(name string) error {
	n := strings.ToLower(strings.TrimSpace(name))
	if !nameRe.MatchString(n) {
		return ErrInvalidName
	}
	if _, reserved := ReservedNames[n]; reserved {
		return ErrInvalidName
	}
	return nil
}

// Create inserts a new group. createdBy is the actor's username (or
// "system" / "bootstrap" for non-human sources).
func (s *Store) Create(ctx context.Context, name, createdBy string) (Group, error) {
	if err := ValidateName(name); err != nil {
		return Group{}, err
	}
	id, err := newID()
	if err != nil {
		return Group{}, fmt.Errorf("groups: id: %w", err)
	}
	now := time.Now().UTC()
	nameLower := strings.ToLower(strings.TrimSpace(name))
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO groups (id, name, created_at, created_by) VALUES (?, ?, ?, ?)`,
		id, nameLower, now.Unix(), createdBy,
	); err != nil {
		if isUniqueViolation(err) {
			return Group{}, ErrNameTaken
		}
		return Group{}, fmt.Errorf("groups: insert: %w", err)
	}
	return Group{ID: id, Name: nameLower, CreatedAt: now, CreatedBy: createdBy}, nil
}

// Get returns the group by name (case-insensitive). ErrNotFound when absent.
func (s *Store) Get(ctx context.Context, name string) (Group, error) {
	nameLower := strings.ToLower(strings.TrimSpace(name))
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, created_at, created_by FROM groups WHERE name = ? COLLATE NOCASE`,
		nameLower,
	)
	return scanGroup(row)
}

// Delete removes a group and its memberships. ON DELETE CASCADE on
// group_members.group_id handles the join cleanup. ErrNotFound when
// absent (guard against silent no-ops in callers).
func (s *Store) Delete(ctx context.Context, name string) error {
	nameLower := strings.ToLower(strings.TrimSpace(name))
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM groups WHERE name = ? COLLATE NOCASE`,
		nameLower,
	)
	if err != nil {
		return fmt.Errorf("groups: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Rename changes a group's name. The ID is unchanged so permission
// rules that reference by id keep matching. Returns ErrNameTaken when
// newName collides, ErrInvalidName when newName fails validation,
// ErrNotFound when oldName is absent.
func (s *Store) Rename(ctx context.Context, oldName, newName string) error {
	if err := ValidateName(newName); err != nil {
		return err
	}
	oldLower := strings.ToLower(strings.TrimSpace(oldName))
	newLower := strings.ToLower(strings.TrimSpace(newName))
	if oldLower == newLower {
		// No-op; still verify existence so callers get a clear error
		// when they typo'd both args identically against a missing group.
		if _, err := s.Get(ctx, oldLower); err != nil {
			return err
		}
		return nil
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE groups SET name = ? WHERE name = ? COLLATE NOCASE`,
		newLower, oldLower,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		return fmt.Errorf("groups: rename: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddMember inserts (group, username) into group_members. ErrNotFound
// when the group doesn't exist; ErrUserUnknown when the user doesn't
// exist; idempotent re-adds are silent successes.
func (s *Store) AddMember(ctx context.Context, name, username string) error {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return ErrUserUnknown
	}
	g, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Unix()
	// Plain INSERT (not "OR IGNORE") so a FOREIGN KEY constraint
	// failure surfaces as ErrUserUnknown. We handle the PK collision
	// case (re-add) as a non-error.
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO group_members (group_id, username, added_at) VALUES (?, ?, ?)`,
		g.ID, username, now,
	)
	if err == nil {
		return nil
	}
	if isUniqueViolation(err) {
		// Already a member; re-add is idempotent.
		return nil
	}
	if isFKViolation(err) {
		return ErrUserUnknown
	}
	return fmt.Errorf("groups: add member: %w", err)
}

// RemoveMember deletes (group, username) from group_members.
// ErrNotFound when the group doesn't exist; ErrNotMember when the
// user wasn't a member.
func (s *Store) RemoveMember(ctx context.Context, name, username string) error {
	username = strings.ToLower(strings.TrimSpace(username))
	g, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM group_members WHERE group_id = ? AND username = ?`,
		g.ID, username,
	)
	if err != nil {
		return fmt.Errorf("groups: remove member: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotMember
	}
	return nil
}

// Members returns every member of name, ordered by added_at.
// ErrNotFound when the group doesn't exist.
func (s *Store) Members(ctx context.Context, name string) ([]Member, error) {
	g, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT username, added_at FROM group_members WHERE group_id = ? ORDER BY added_at ASC, username ASC`,
		g.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("groups: list members: %w", err)
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		var addedAt int64
		if err := rows.Scan(&m.Username, &addedAt); err != nil {
			return nil, fmt.Errorf("groups: scan member: %w", err)
		}
		m.AddedAt = time.Unix(addedAt, 0).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// MemberOf returns the names of every group that contains username.
// Empty slice when the user belongs to none (or doesn't exist; this
// is a query-side helper, not a user-existence check).
func (s *Store) MemberOf(ctx context.Context, username string) ([]string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	rows, err := s.db.QueryContext(ctx,
		`SELECT g.name
		   FROM groups g
		   JOIN group_members m ON m.group_id = g.id
		  WHERE m.username = ?
		  ORDER BY g.name ASC`,
		username,
	)
	if err != nil {
		return nil, fmt.Errorf("groups: member-of: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("groups: scan member-of: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// List returns every group in the project, ordered by name.
func (s *Store) List(ctx context.Context) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, created_at, created_by FROM groups ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("groups: list: %w", err)
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ---- helpers ----

// scanGroup reads one row in the canonical column order. Accepts
// *sql.Row and *sql.Rows via the rowScanner interface.
func scanGroup(r rowScanner) (Group, error) {
	var g Group
	var createdAt int64
	if err := r.Scan(&g.ID, &g.Name, &createdAt, &g.CreatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Group{}, ErrNotFound
		}
		return Group{}, fmt.Errorf("groups: scan: %w", err)
	}
	g.CreatedAt = time.Unix(createdAt, 0).UTC()
	return g, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

// newID mints a 26-char alphanumeric identifier. We don't need a real
// ULID's time-sort property here — a random opaque string is fine. The
// length matches what invitations and tokens use, so log lines look
// consistent.
func newID() (string, error) {
	var b [13]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "grp_" + hex.EncodeToString(b[:]), nil
}

// isUniqueViolation reports whether err is a SQLite unique-constraint
// failure. modernc.org/sqlite surfaces these as text in err.Error().
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed") ||
		strings.Contains(s, "constraint failed: UNIQUE")
}

// isFKViolation reports whether err is a SQLite foreign-key failure.
// Used to distinguish "you tried to add a non-existent user" from
// other errors when adding a member.
func isFKViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "FOREIGN KEY constraint failed") ||
		strings.Contains(s, "constraint failed: FOREIGN KEY")
}
