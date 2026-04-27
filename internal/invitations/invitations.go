// Package invitations stores one-time invite codes. An admin mints an
// invitation; a prospective user redeems it at /invite/<id> to claim
// their account. Single-use in v0: redeeming consumes the row.
//
// The store itself is purely mechanical — the handler that calls
// Redeem() is responsible for creating the user + first token in the
// same transaction, because a redeemed-but-userless invitation is a
// strictly worse state than a failed redeem.
package invitations

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Role mirrors internal/auth.Kind values. Kept as a string alias to
// avoid an import cycle (auth may eventually depend on invitations
// for the bootstrap flow).
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleBot    Role = "bot"
)

// ValidRole reports whether r is one of the three recognised values.
func ValidRole(r Role) bool {
	return r == RoleAdmin || r == RoleMember || r == RoleBot
}

// Invitation is one row.
type Invitation struct {
	ID         string     `json:"id"`
	Role       Role       `json:"role"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Label      string     `json:"label,omitempty"`
	RedeemedAt *time.Time `json:"redeemed_at,omitempty"`
	RedeemedBy string     `json:"redeemed_by,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// Status derives a label from the row fields. Precedence: revoked >
// redeemed > expired > active. Useful for UI rendering.
func (i *Invitation) Status() string {
	if i.RevokedAt != nil {
		return "revoked"
	}
	if i.RedeemedAt != nil {
		return "redeemed"
	}
	if time.Now().After(i.ExpiresAt) {
		return "expired"
	}
	return "active"
}

// Standard errors. Handlers map these to HTTP status codes.
var (
	ErrNotFound        = errors.New("invitations: not found")
	ErrExpired         = errors.New("invitations: expired")
	ErrAlreadyRedeemed = errors.New("invitations: already redeemed")
	ErrRevoked         = errors.New("invitations: revoked")
	ErrInvalidRole     = errors.New("invitations: role must be admin, member, or bot")
)

// BootstrapCreator is the sentinel value in created_by that marks a
// bootstrap invitation. The BootstrapActive helper uses it to locate
// the in-flight first-admin invite on reboot.
const BootstrapCreator = "bootstrap"

const schemaSQL = `
CREATE TABLE IF NOT EXISTS invitations (
    id           TEXT PRIMARY KEY,
    role         TEXT NOT NULL CHECK (role IN ('admin','member','bot')),
    created_by   TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    label        TEXT,
    redeemed_at  INTEGER,
    redeemed_by  TEXT,
    revoked_at   INTEGER
) STRICT;

CREATE INDEX IF NOT EXISTS idx_invitations_active_ids
    ON invitations(id) WHERE redeemed_at IS NULL AND revoked_at IS NULL;

-- Partial-unique so two concurrent boots can't both mint a fresh
-- bootstrap invitation. Second insert hits UniqueViolation; caller
-- SELECTs the existing row instead.
CREATE UNIQUE INDEX IF NOT EXISTS idx_invitations_one_bootstrap
    ON invitations(created_by)
    WHERE created_by = 'bootstrap' AND redeemed_at IS NULL AND revoked_at IS NULL;
`

// Store owns the invitations table.
type Store struct {
	db *sql.DB
}

// NewStore migrates and returns a store.
func NewStore(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("invitations: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// CreateParams bundles inputs for Create.
type CreateParams struct {
	Role      Role
	CreatedBy string        // admin username, or BootstrapCreator sentinel
	ExpiresIn time.Duration // stored as expires_at = now + ExpiresIn
	Label     string
}

// Create inserts a new invitation. Returns the populated row.
func (s *Store) Create(p CreateParams) (*Invitation, error) {
	if !ValidRole(p.Role) {
		return nil, ErrInvalidRole
	}
	if p.CreatedBy == "" {
		return nil, fmt.Errorf("invitations: created_by required")
	}
	if p.ExpiresIn <= 0 {
		p.ExpiresIn = 7 * 24 * time.Hour
	}
	id, err := generateID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = s.db.Exec(
		`INSERT INTO invitations (id, role, created_by, created_at, expires_at, label)
		 VALUES (?, ?, ?, ?, ?, NULLIF(?, ''))`,
		id, string(p.Role), p.CreatedBy, now.Unix(), now.Add(p.ExpiresIn).Unix(), p.Label,
	)
	if err != nil {
		return nil, fmt.Errorf("invitations: insert: %w", err)
	}
	return s.Get(id)
}

// Get returns a single invitation by id.
func (s *Store) Get(id string) (*Invitation, error) {
	row := s.db.QueryRow(
		`SELECT id, role, created_by, created_at, expires_at, label,
		        redeemed_at, redeemed_by, revoked_at
		 FROM invitations WHERE id = ?`, id)
	inv, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return inv, err
}

// List returns every invitation, newest first. When includeInactive is
// false, revoked/redeemed/expired rows are filtered out.
func (s *Store) List(includeInactive bool) ([]*Invitation, error) {
	var q string
	var args []any
	if includeInactive {
		q = `SELECT id, role, created_by, created_at, expires_at, label,
		           redeemed_at, redeemed_by, revoked_at
		    FROM invitations ORDER BY created_at DESC`
	} else {
		q = `SELECT id, role, created_by, created_at, expires_at, label,
		           redeemed_at, redeemed_by, revoked_at
		    FROM invitations
		    WHERE redeemed_at IS NULL AND revoked_at IS NULL AND expires_at > ?
		    ORDER BY created_at DESC`
		args = append(args, time.Now().UTC().Unix())
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Invitation
	for rows.Next() {
		inv, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// Revoke soft-deletes an invitation. Idempotent — calling twice keeps
// the first revoked_at timestamp.
func (s *Store) Revoke(id string) error {
	res, err := s.db.Exec(
		`UPDATE invitations SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`,
		time.Now().UTC().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Redeem atomically marks the invitation as used by `username`. Single
// UPDATE with every precondition in the WHERE clause handles the
// concurrency window; the row is considered consumed only when
// RowsAffected() == 1.
//
// Caller is responsible for coordinating user + token creation in the
// same sql.Tx so a redeemed-but-userless row never exists.
//
// On precondition-miss, Redeem re-reads the row to classify the error
// as Expired / AlreadyRedeemed / Revoked / NotFound. This costs one
// extra SELECT on the unhappy path — acceptable for the clarity of a
// typed error to the handler.
func (s *Store) Redeem(id, username string) (*Invitation, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return nil, fmt.Errorf("invitations: username required")
	}
	now := time.Now().UTC().Unix()
	res, err := s.db.Exec(
		`UPDATE invitations
		   SET redeemed_at = ?, redeemed_by = ?
		 WHERE id = ?
		   AND redeemed_at IS NULL
		   AND revoked_at IS NULL
		   AND expires_at > ?`,
		now, username, id, now)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return s.Get(id)
	}
	// Precondition miss — classify.
	inv, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if inv.RevokedAt != nil {
		return nil, ErrRevoked
	}
	if inv.RedeemedAt != nil {
		return nil, ErrAlreadyRedeemed
	}
	return nil, ErrExpired
}

// BootstrapActive returns the currently-active bootstrap invitation
// (role=admin, created_by=BootstrapCreator, not redeemed/revoked, not
// expired), if one exists. Nil + nil on miss. Used by the serve-path
// to reuse an existing first-admin invite across restarts.
func (s *Store) BootstrapActive() (*Invitation, error) {
	row := s.db.QueryRow(
		`SELECT id, role, created_by, created_at, expires_at, label,
		        redeemed_at, redeemed_by, revoked_at
		 FROM invitations
		 WHERE created_by = ?
		   AND redeemed_at IS NULL
		   AND revoked_at IS NULL
		   AND expires_at > ?
		 ORDER BY created_at DESC LIMIT 1`,
		BootstrapCreator, time.Now().UTC().Unix())
	inv, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return inv, err
}

// -------- helpers --------

type rowScanner interface {
	Scan(dest ...any) error
}

func scan(r rowScanner) (*Invitation, error) {
	var (
		inv         Invitation
		role        string
		createdAt   int64
		expiresAt   int64
		label       sql.NullString
		redeemedAt  sql.NullInt64
		redeemedBy  sql.NullString
		revokedAt   sql.NullInt64
	)
	if err := r.Scan(&inv.ID, &role, &inv.CreatedBy, &createdAt, &expiresAt,
		&label, &redeemedAt, &redeemedBy, &revokedAt); err != nil {
		return nil, err
	}
	inv.Role = Role(role)
	inv.CreatedAt = time.Unix(createdAt, 0).UTC()
	inv.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	if label.Valid {
		inv.Label = label.String
	}
	if redeemedAt.Valid {
		t := time.Unix(redeemedAt.Int64, 0).UTC()
		inv.RedeemedAt = &t
	}
	if redeemedBy.Valid {
		inv.RedeemedBy = redeemedBy.String
	}
	if revokedAt.Valid {
		t := time.Unix(revokedAt.Int64, 0).UTC()
		inv.RevokedAt = &t
	}
	return &inv, nil
}

// generateID returns a random "inv_" + 16 base62 chars (URL-safe).
// The `inv_` prefix makes it immediately obvious in logs + URLs what
// the token represents; the 16-char body gives ~95 bits of entropy.
func generateID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("invitations: rand: %w", err)
	}
	// URLEncoding with padding stripped; replace - / _ with 0-9/a-z
	// scope? Simpler: use URLEncoding and strip '=' — remains URL-safe.
	raw := base64.RawURLEncoding.EncodeToString(b[:])
	if len(raw) > 16 {
		raw = raw[:16]
	}
	return "inv_" + raw, nil
}
