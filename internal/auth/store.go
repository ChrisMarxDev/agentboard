package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Standard errors. Handlers translate these to HTTP status codes.
var (
	ErrNotFound       = errors.New("not found")
	ErrNameTaken      = errors.New("name already in use")
	ErrRevoked        = errors.New("identity revoked")
	ErrSessionExpired = errors.New("session expired")
	ErrCodeInvalid    = errors.New("bootstrap code invalid or already used")
)

// Kind enumerates the two identity realms.
type Kind string

const (
	KindAdmin Kind = "admin"
	KindAgent Kind = "agent"
)

// AccessMode controls how rules are evaluated for an agent identity.
type AccessMode string

const (
	ModeAllowAll       AccessMode = "allow_all"
	ModeRestrictToList AccessMode = "restrict_to_list"
)

// Identity is one row in auth_identities.
//
// TokenHash and PasswordHash are never exposed over HTTP; they're internal
// fields used by the middleware and login handler.
type Identity struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Kind         Kind       `json:"kind"`
	TokenHash    string     `json:"-"`
	PasswordHash string     `json:"-"`
	AccessMode   AccessMode `json:"access_mode"`
	Rules        []Rule     `json:"rules"`
	CreatedAt    time.Time  `json:"created_at"`
	CreatedBy    string     `json:"created_by,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
}

// Session is one row in auth_sessions.
type Session struct {
	ID         string
	IdentityID string
	CSRFToken  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	UserAgent  string
	IP         string
}

// BootstrapCode is one row in auth_bootstrap_codes.
type BootstrapCode struct {
	ID        string
	CodeHash  string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
	Note      string
}

// Store owns the auth-related tables. It reuses the data store's *sql.DB so
// backups, WAL mode, and transactions apply uniformly across both realms.
type Store struct {
	db *sql.DB
}

// NewStore runs the auth migrations against db and returns a Store.
func NewStore(db *sql.DB) (*Store, error) {
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// ------------------------------------------------------------------
// Identities
// ------------------------------------------------------------------

// CreateIdentityParams bundles inputs for CreateIdentity. One of TokenHash
// (agent) or PasswordHash (admin) must be set to match Kind.
type CreateIdentityParams struct {
	Name         string
	Kind         Kind
	TokenHash    string
	PasswordHash string
	AccessMode   AccessMode
	Rules        []Rule
	CreatedBy    string
}

// CreateIdentity inserts a new row. Returns ErrNameTaken if the name collides.
func (s *Store) CreateIdentity(p CreateIdentityParams) (*Identity, error) {
	if p.AccessMode == "" {
		p.AccessMode = ModeAllowAll
	}
	rulesJSON, err := json.Marshal(ensureRules(p.Rules))
	if err != nil {
		return nil, fmt.Errorf("marshal rules: %w", err)
	}

	now := time.Now().UTC()
	id := uuid.NewString()

	_, err = s.db.Exec(
		`INSERT INTO auth_identities
		 (id, name, kind, token_hash, password_hash, access_mode, rules_json, created_at, created_by)
		 VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''))`,
		id, p.Name, string(p.Kind), p.TokenHash, p.PasswordHash,
		string(p.AccessMode), string(rulesJSON), now.Unix(), p.CreatedBy,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrNameTaken
		}
		return nil, fmt.Errorf("insert identity: %w", err)
	}

	return s.GetIdentity(id)
}

// GetIdentity returns the identity with the given id. Returns ErrNotFound
// if no row matches.
func (s *Store) GetIdentity(id string) (*Identity, error) {
	return s.queryIdentity(`SELECT id, name, kind, token_hash, password_hash,
	                               access_mode, rules_json, created_at, created_by,
	                               last_used_at, revoked_at
	                        FROM auth_identities WHERE id = ?`, id)
}

// GetIdentityByName returns the identity with the given name, or ErrNotFound.
func (s *Store) GetIdentityByName(name string) (*Identity, error) {
	return s.queryIdentity(`SELECT id, name, kind, token_hash, password_hash,
	                               access_mode, rules_json, created_at, created_by,
	                               last_used_at, revoked_at
	                        FROM auth_identities WHERE name = ?`, name)
}

// GetIdentityByTokenHash returns the agent identity whose token hash matches.
// Returns ErrNotFound if no match, or ErrRevoked if the identity is revoked.
func (s *Store) GetIdentityByTokenHash(hash string) (*Identity, error) {
	ident, err := s.queryIdentity(`SELECT id, name, kind, token_hash, password_hash,
	                                      access_mode, rules_json, created_at, created_by,
	                                      last_used_at, revoked_at
	                               FROM auth_identities
	                               WHERE token_hash = ?`, hash)
	if err != nil {
		return nil, err
	}
	if ident.RevokedAt != nil {
		return nil, ErrRevoked
	}
	return ident, nil
}

// ListIdentities returns all identities. Revoked ones are included (UI
// filters them).
func (s *Store) ListIdentities() ([]*Identity, error) {
	rows, err := s.db.Query(`SELECT id, name, kind, token_hash, password_hash,
	                                access_mode, rules_json, created_at, created_by,
	                                last_used_at, revoked_at
	                         FROM auth_identities
	                         ORDER BY kind DESC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Identity
	for rows.Next() {
		ident, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ident)
	}
	return out, rows.Err()
}

// HasAdmin returns true if at least one non-revoked admin identity exists.
// Used by the setup flow to know whether it's the first-run case.
func (s *Store) HasAdmin() (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM auth_identities
	                      WHERE kind = 'admin' AND revoked_at IS NULL`).Scan(&n)
	return n > 0, err
}

// UpdateIdentity updates mutable fields. Only name, access_mode, and rules
// are updatable via this method; secrets have dedicated rotate/password
// endpoints.
type UpdateIdentityParams struct {
	Name       *string
	AccessMode *AccessMode
	Rules      *[]Rule
}

func (s *Store) UpdateIdentity(id string, p UpdateIdentityParams) (*Identity, error) {
	existing, err := s.GetIdentity(id)
	if err != nil {
		return nil, err
	}
	if p.Name != nil {
		existing.Name = *p.Name
	}
	if p.AccessMode != nil {
		existing.AccessMode = *p.AccessMode
	}
	if p.Rules != nil {
		existing.Rules = *p.Rules
	}
	rulesJSON, err := json.Marshal(ensureRules(existing.Rules))
	if err != nil {
		return nil, fmt.Errorf("marshal rules: %w", err)
	}
	_, err = s.db.Exec(
		`UPDATE auth_identities SET name = ?, access_mode = ?, rules_json = ? WHERE id = ?`,
		existing.Name, string(existing.AccessMode), string(rulesJSON), id,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrNameTaken
		}
		return nil, err
	}
	return s.GetIdentity(id)
}

// RotateToken sets a new token hash for an agent identity.
func (s *Store) RotateToken(id, newTokenHash string) error {
	res, err := s.db.Exec(
		`UPDATE auth_identities SET token_hash = ? WHERE id = ? AND kind = 'agent'`,
		newTokenHash, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPassword replaces an admin identity's password hash.
func (s *Store) SetPassword(id, newPasswordHash string) error {
	res, err := s.db.Exec(
		`UPDATE auth_identities SET password_hash = ? WHERE id = ? AND kind = 'admin'`,
		newPasswordHash, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Revoke marks an identity as revoked. Revocation is soft — the row is kept
// for audit. Revoked identities fail token lookups and cannot log in.
func (s *Store) Revoke(id string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.Exec(
		`UPDATE auth_identities SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		now, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchLastUsed records that an identity was used in a request. Called
// opportunistically from the token-auth middleware; not a hot path per
// request (the middleware coalesces updates).
func (s *Store) TouchLastUsed(id string) error {
	_, err := s.db.Exec(
		`UPDATE auth_identities SET last_used_at = ? WHERE id = ?`,
		time.Now().UTC().Unix(), id,
	)
	return err
}

// ------------------------------------------------------------------
// Sessions
// ------------------------------------------------------------------

// CreateSession opens an admin session for the given identity.
func (s *Store) CreateSession(identityID, userAgent, ip string, ttl time.Duration) (*Session, error) {
	sid, err := GenerateSessionID()
	if err != nil {
		return nil, err
	}
	csrf, err := GenerateSessionID() // reuse the 32-byte generator; shape is the same
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	sess := &Session{
		ID:         sid,
		IdentityID: identityID,
		CSRFToken:  csrf,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(ttl),
		UserAgent:  userAgent,
		IP:         ip,
	}
	_, err = s.db.Exec(
		`INSERT INTO auth_sessions
		 (id, identity_id, csrf_token, created_at, last_seen_at, expires_at, user_agent, ip)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.IdentityID, sess.CSRFToken,
		sess.CreatedAt.Unix(), sess.LastSeenAt.Unix(), sess.ExpiresAt.Unix(),
		sess.UserAgent, sess.IP,
	)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return sess, nil
}

// GetSession fetches a session by ID. Returns ErrNotFound if no match,
// ErrSessionExpired if the session exists but has aged out (caller should
// delete it).
func (s *Store) GetSession(id string) (*Session, error) {
	var sess Session
	var createdAt, lastSeenAt, expiresAt int64
	err := s.db.QueryRow(
		`SELECT id, identity_id, csrf_token, created_at, last_seen_at, expires_at,
		        COALESCE(user_agent, ''), COALESCE(ip, '')
		 FROM auth_sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.IdentityID, &sess.CSRFToken,
		&createdAt, &lastSeenAt, &expiresAt, &sess.UserAgent, &sess.IP)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.CreatedAt = time.Unix(createdAt, 0).UTC()
	sess.LastSeenAt = time.Unix(lastSeenAt, 0).UTC()
	sess.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	if time.Now().After(sess.ExpiresAt) {
		return &sess, ErrSessionExpired
	}
	return &sess, nil
}

// TouchSession bumps last_seen_at to now. Called on every admin request.
func (s *Store) TouchSession(id string) error {
	_, err := s.db.Exec(
		`UPDATE auth_sessions SET last_seen_at = ? WHERE id = ?`,
		time.Now().UTC().Unix(), id,
	)
	return err
}

// DeleteSession removes a session.
func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE id = ?`, id)
	return err
}

// DeleteSessionsForIdentity removes all sessions owned by an identity.
// Called on password change, revocation, and admin reset.
func (s *Store) DeleteSessionsForIdentity(identityID string) error {
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE identity_id = ?`, identityID)
	return err
}

// PruneSessions deletes expired sessions. Called by a background job.
func (s *Store) PruneSessions() error {
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE expires_at < ?`,
		time.Now().UTC().Unix())
	return err
}

// ------------------------------------------------------------------
// Bootstrap codes
// ------------------------------------------------------------------

// CreateBootstrapCode issues a new one-time code. Returns the plaintext
// (printed to the operator) and the stored row. The plaintext is NOT
// recoverable from the DB — only the hash is stored.
func (s *Store) CreateBootstrapCode(ttl time.Duration, note string) (string, *BootstrapCode, error) {
	code, err := GenerateBootstrapCode()
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()
	bc := &BootstrapCode{
		ID:        uuid.NewString(),
		CodeHash:  HashToken(code),
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
		Note:      note,
	}
	_, err = s.db.Exec(
		`INSERT INTO auth_bootstrap_codes (id, code_hash, created_at, expires_at, note)
		 VALUES (?, ?, ?, ?, NULLIF(?, ''))`,
		bc.ID, bc.CodeHash, bc.CreatedAt.Unix(), bc.ExpiresAt.Unix(), note,
	)
	if err != nil {
		return "", nil, fmt.Errorf("insert bootstrap code: %w", err)
	}
	return code, bc, nil
}

// ConsumeBootstrapCode validates and marks a code as used atomically.
// Returns ErrCodeInvalid if the code is unknown, expired, or already used.
func (s *Store) ConsumeBootstrapCode(code string) error {
	hash := HashToken(code)
	now := time.Now().UTC().Unix()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var id string
	var expiresAt int64
	var usedAt sql.NullInt64
	err = tx.QueryRow(
		`SELECT id, expires_at, used_at FROM auth_bootstrap_codes WHERE code_hash = ?`,
		hash,
	).Scan(&id, &expiresAt, &usedAt)
	if err == sql.ErrNoRows {
		return ErrCodeInvalid
	}
	if err != nil {
		return err
	}
	if usedAt.Valid || expiresAt < now {
		return ErrCodeInvalid
	}
	if _, err := tx.Exec(
		`UPDATE auth_bootstrap_codes SET used_at = ? WHERE id = ?`, now, id,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ListBootstrapCodes returns all non-expired, unused codes (hash only — the
// plaintext cannot be recovered). Used by the admin UI to show outstanding
// codes.
func (s *Store) ListBootstrapCodes() ([]*BootstrapCode, error) {
	now := time.Now().UTC().Unix()
	rows, err := s.db.Query(
		`SELECT id, code_hash, created_at, expires_at, used_at, COALESCE(note, '')
		 FROM auth_bootstrap_codes
		 WHERE used_at IS NULL AND expires_at >= ?
		 ORDER BY created_at DESC`, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*BootstrapCode
	for rows.Next() {
		var bc BootstrapCode
		var createdAt, expiresAt int64
		var usedAt sql.NullInt64
		if err := rows.Scan(&bc.ID, &bc.CodeHash, &createdAt, &expiresAt, &usedAt, &bc.Note); err != nil {
			return nil, err
		}
		bc.CreatedAt = time.Unix(createdAt, 0).UTC()
		bc.ExpiresAt = time.Unix(expiresAt, 0).UTC()
		if usedAt.Valid {
			t := time.Unix(usedAt.Int64, 0).UTC()
			bc.UsedAt = &t
		}
		out = append(out, &bc)
	}
	return out, rows.Err()
}

// DeleteBootstrapCode removes a code. Used when an admin wants to revoke
// an outstanding code without waiting for expiry.
func (s *Store) DeleteBootstrapCode(id string) error {
	_, err := s.db.Exec(`DELETE FROM auth_bootstrap_codes WHERE id = ?`, id)
	return err
}

// PruneBootstrapCodes deletes expired and used codes older than a week.
// Kept for a while after use for audit.
func (s *Store) PruneBootstrapCodes() error {
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour).Unix()
	_, err := s.db.Exec(
		`DELETE FROM auth_bootstrap_codes
		 WHERE (used_at IS NOT NULL AND used_at < ?) OR expires_at < ?`,
		cutoff, cutoff,
	)
	return err
}

// ------------------------------------------------------------------
// helpers
// ------------------------------------------------------------------

// scanIdentity reads one row from either a *sql.Row or *sql.Rows (both
// satisfy the tiny rowScanner interface).
func scanIdentity(r rowScanner) (*Identity, error) {
	var ident Identity
	var tokenHash, passwordHash, createdBy sql.NullString
	var createdAt int64
	var lastUsedAt, revokedAt sql.NullInt64
	var rulesJSON string

	err := r.Scan(&ident.ID, &ident.Name, &ident.Kind,
		&tokenHash, &passwordHash, &ident.AccessMode, &rulesJSON,
		&createdAt, &createdBy, &lastUsedAt, &revokedAt)
	if err != nil {
		return nil, err
	}

	ident.TokenHash = tokenHash.String
	ident.PasswordHash = passwordHash.String
	ident.CreatedAt = time.Unix(createdAt, 0).UTC()
	ident.CreatedBy = createdBy.String

	if lastUsedAt.Valid {
		t := time.Unix(lastUsedAt.Int64, 0).UTC()
		ident.LastUsedAt = &t
	}
	if revokedAt.Valid {
		t := time.Unix(revokedAt.Int64, 0).UTC()
		ident.RevokedAt = &t
	}
	ident.Rules = ensureRules(nil)
	if rulesJSON != "" {
		if err := json.Unmarshal([]byte(rulesJSON), &ident.Rules); err != nil {
			return nil, fmt.Errorf("unmarshal rules for %s: %w", ident.ID, err)
		}
	}
	return &ident, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) queryIdentity(query string, args ...any) (*Identity, error) {
	row := s.db.QueryRow(query, args...)
	ident, err := scanIdentity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return ident, nil
}

// isUniqueViolation recognizes SQLite's unique-constraint error without
// requiring a driver-specific import. We look at the error string because
// modernc/sqlite surfaces these as plain errors.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsAny(msg, "UNIQUE constraint failed", "constraint failed: UNIQUE")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}
