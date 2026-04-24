// Package teams implements role-group identity — @marketing, @design,
// @oncall — layered on top of per-user @alice mentions. A team is a
// durable, named group of usernames. Mentions of a team name expand to
// every active member when the inbox dispatcher runs, so a single
// message reaches the group without paging individual names.
//
// Team slugs share the same grammar as usernames (see
// internal/auth.usernameRE). That's intentional: the mention regex
// doesn't need to differentiate user vs team names at the text layer;
// resolution happens at the producer boundary (users win the slug space
// over teams; reserved names win over both).
package teams

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Team is one row.
type Team struct {
	Slug        string    `json:"slug"`
	DisplayName string    `json:"display_name,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by,omitempty"`
	Members     []Member  `json:"members,omitempty"`
}

// Member is one row of team_members.
type Member struct {
	Username string    `json:"username"`
	Role     string    `json:"role,omitempty"`
	AddedAt  time.Time `json:"added_at"`
}

// Reserved names that never refer to a stored team. @all, @admins,
// @agents resolve to dynamic sets of users at dispatch time. Callers
// creating a team with any of these slugs are rejected.
//
// Lowercase; compared against a lowercased slug at every callsite.
var reservedSlugs = map[string]bool{
	"all":    true,
	"admins": true,
	"agents": true,
	"here":   true,
}

// IsReserved reports whether a slug is one of the special built-in
// pseudo-teams (@all, @admins, @agents, @here).
func IsReserved(slug string) bool {
	return reservedSlugs[strings.ToLower(strings.TrimSpace(slug))]
}

// slugRE mirrors usernameRE from the auth package. Kept local to avoid
// an import cycle, but the grammar MUST stay in lockstep.
var slugRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// ValidateSlug returns a non-nil error when slug isn't a valid team
// name. Reserved names are rejected here so create/rename operations
// can't produce a team that shadows @all.
func ValidateSlug(slug string) error {
	s := strings.ToLower(strings.TrimSpace(slug))
	if !slugRE.MatchString(s) {
		return ErrInvalidSlug
	}
	if reservedSlugs[s] {
		return ErrReservedSlug
	}
	return nil
}

// Standard errors.
var (
	ErrNotFound     = errors.New("teams: not found")
	ErrSlugTaken    = errors.New("teams: slug already in use")
	ErrInvalidSlug  = errors.New("teams: invalid slug (lowercase a-z, 0-9, _ or -; max 32 chars)")
	ErrReservedSlug = errors.New("teams: slug is reserved (@all, @admins, @agents, @here)")
	ErrNotMember    = errors.New("teams: user is not a member")
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS teams (
    slug         TEXT NOT NULL PRIMARY KEY COLLATE NOCASE,
    display_name TEXT,
    description  TEXT,
    created_at   INTEGER NOT NULL,
    created_by   TEXT
) STRICT;

CREATE TABLE IF NOT EXISTS team_members (
    team_slug TEXT NOT NULL,
    username  TEXT NOT NULL COLLATE NOCASE,
    role      TEXT,
    added_at  INTEGER NOT NULL,
    PRIMARY KEY (team_slug, username)
) STRICT;

CREATE INDEX IF NOT EXISTS idx_team_members_user ON team_members(username);
`

// Store owns the teams and team_members tables. Thread-safe: all
// methods go through *sql.DB which handles its own locking.
type Store struct {
	db *sql.DB
}

// NewStore migrates the schema and returns a Store.
func NewStore(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("teams: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// CreateParams bundles inputs for Create.
type CreateParams struct {
	Slug        string
	DisplayName string
	Description string
	CreatedBy   string
}

// Create inserts a new team. Validates slug; rejects reserved names and
// duplicates.
//
// The caller that wires admin routes must additionally ensure the slug
// doesn't already belong to a user — user slugs win over teams. That
// check requires the auth store and is enforced by the handler, not
// here (this package has no auth dependency).
func (s *Store) Create(p CreateParams) (*Team, error) {
	p.Slug = strings.ToLower(strings.TrimSpace(p.Slug))
	if err := ValidateSlug(p.Slug); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Unix()
	_, err := s.db.Exec(
		`INSERT INTO teams (slug, display_name, description, created_at, created_by)
		 VALUES (?, NULLIF(?, ''), NULLIF(?, ''), ?, NULLIF(?, ''))`,
		p.Slug, p.DisplayName, p.Description, now, p.CreatedBy,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrSlugTaken
		}
		return nil, fmt.Errorf("teams: insert: %w", err)
	}
	return s.Get(p.Slug)
}

// Get returns a team with its members loaded.
func (s *Store) Get(slug string) (*Team, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	row := s.db.QueryRow(
		`SELECT slug, display_name, description, created_at, created_by
		 FROM teams WHERE slug = ? COLLATE NOCASE`, slug)
	t, err := scanTeam(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	members, err := s.ListMembers(slug)
	if err != nil {
		return nil, err
	}
	t.Members = members
	return t, nil
}

// Exists is a cheap slug-check used by callers that need to resolve a
// mention without paying the members read. Returns false on any error
// (including ErrNotFound) so the caller can treat it as "not a team".
func (s *Store) Exists(slug string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return false
	}
	var x string
	err := s.db.QueryRow(`SELECT slug FROM teams WHERE slug = ? COLLATE NOCASE`, slug).Scan(&x)
	return err == nil
}

// List returns every team, ordered by slug. Members are NOT populated —
// callers that need members should Get() each team individually or use
// ListWithMembers when the cost is acceptable.
func (s *Store) List() ([]*Team, error) {
	rows, err := s.db.Query(
		`SELECT slug, display_name, description, created_at, created_by
		 FROM teams ORDER BY slug ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Team
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListWithMembers returns every team with its members array populated.
// One round trip for teams + one per team for members; fine at v0
// volumes (tens of teams).
func (s *Store) ListWithMembers() ([]*Team, error) {
	teams, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, t := range teams {
		members, err := s.ListMembers(t.Slug)
		if err != nil {
			return nil, err
		}
		t.Members = members
	}
	return teams, nil
}

// UpdateParams covers the mutable fields. Slug is immutable via normal
// APIs (same invariant as usernames); rename would need a transaction
// and an explicit CLI step and is deferred.
type UpdateParams struct {
	DisplayName *string
	Description *string
}

// Update mutates display_name / description. Missing pointer = no
// change.
func (s *Store) Update(slug string, p UpdateParams) (*Team, error) {
	existing, err := s.Get(slug)
	if err != nil {
		return nil, err
	}
	if p.DisplayName != nil {
		existing.DisplayName = *p.DisplayName
	}
	if p.Description != nil {
		existing.Description = *p.Description
	}
	_, err = s.db.Exec(
		`UPDATE teams SET display_name = NULLIF(?, ''), description = NULLIF(?, '')
		 WHERE slug = ? COLLATE NOCASE`,
		existing.DisplayName, existing.Description, existing.Slug,
	)
	if err != nil {
		return nil, err
	}
	return s.Get(existing.Slug)
}

// Delete removes a team and all member rows. Cascade is explicit (no
// FK constraint, since SQLite cascades were flaky across older builds).
func (s *Store) Delete(slug string) error {
	slug = strings.ToLower(strings.TrimSpace(slug))
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM team_members WHERE team_slug = ? COLLATE NOCASE`, slug); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM teams WHERE slug = ? COLLATE NOCASE`, slug)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// -------- members --------

// AddMemberParams bundles inputs for AddMember.
type AddMemberParams struct {
	Slug     string
	Username string
	Role     string
}

// AddMember inserts a row into team_members. Idempotent — calling twice
// with the same (slug, username) is a no-op (PK collision, swallowed).
// The role field updates on re-insert so a second call with a new role
// is effectively an upsert. The caller is responsible for verifying
// that the username exists (this package has no auth dependency).
func (s *Store) AddMember(p AddMemberParams) error {
	p.Slug = strings.ToLower(strings.TrimSpace(p.Slug))
	p.Username = strings.ToLower(strings.TrimSpace(p.Username))
	if p.Slug == "" || p.Username == "" {
		return fmt.Errorf("teams: slug and username required")
	}
	// Check team exists first — returns ErrNotFound otherwise.
	if !s.Exists(p.Slug) {
		return ErrNotFound
	}
	now := time.Now().UTC().Unix()
	_, err := s.db.Exec(
		`INSERT INTO team_members (team_slug, username, role, added_at)
		 VALUES (?, ?, NULLIF(?, ''), ?)
		 ON CONFLICT(team_slug, username) DO UPDATE SET role = excluded.role`,
		p.Slug, p.Username, p.Role, now,
	)
	if err != nil {
		return fmt.Errorf("teams: insert member: %w", err)
	}
	return nil
}

// RemoveMember deletes one member row. Returns ErrNotMember if the row
// didn't exist (so callers can 404 meaningfully).
func (s *Store) RemoveMember(slug, username string) error {
	slug = strings.ToLower(strings.TrimSpace(slug))
	username = strings.ToLower(strings.TrimSpace(username))
	res, err := s.db.Exec(
		`DELETE FROM team_members WHERE team_slug = ? COLLATE NOCASE AND username = ? COLLATE NOCASE`,
		slug, username)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotMember
	}
	return nil
}

// ListMembers returns every member of a team, ordered by added_at.
func (s *Store) ListMembers(slug string) ([]Member, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	rows, err := s.db.Query(
		`SELECT username, role, added_at FROM team_members
		 WHERE team_slug = ? COLLATE NOCASE ORDER BY added_at ASC, username ASC`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var (
			m       Member
			role    sql.NullString
			addedAt int64
		)
		if err := rows.Scan(&m.Username, &role, &addedAt); err != nil {
			return nil, err
		}
		if role.Valid {
			m.Role = role.String
		}
		m.AddedAt = time.Unix(addedAt, 0).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// MemberUsernames returns just the usernames for a slug, lower-cased.
// Cheaper than ListMembers when the caller only needs names — used by
// the mention dispatcher.
func (s *Store) MemberUsernames(slug string) ([]string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	rows, err := s.db.Query(
		`SELECT username FROM team_members WHERE team_slug = ? COLLATE NOCASE ORDER BY added_at ASC`,
		slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, strings.ToLower(u))
	}
	return out, rows.Err()
}

// TeamsForUser returns every team a user belongs to, by slug. Useful
// for the admin user-edit page and for kanban "is this card assigned
// to a team I'm on" queries.
func (s *Store) TeamsForUser(username string) ([]string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	rows, err := s.db.Query(
		`SELECT team_slug FROM team_members WHERE username = ? COLLATE NOCASE ORDER BY team_slug ASC`,
		username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// -------- scanners / helpers --------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTeam(r rowScanner) (*Team, error) {
	var (
		t         Team
		display   sql.NullString
		desc      sql.NullString
		createdAt int64
		createdBy sql.NullString
	)
	if err := r.Scan(&t.Slug, &display, &desc, &createdAt, &createdBy); err != nil {
		return nil, err
	}
	if display.Valid {
		t.DisplayName = display.String
	}
	if desc.Valid {
		t.Description = desc.String
	}
	if createdBy.Valid {
		t.CreatedBy = createdBy.String
	}
	t.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &t, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	return strings.Contains(m, "UNIQUE constraint failed") ||
		strings.Contains(m, "constraint failed: UNIQUE")
}
