package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SessionPrefix prefixes every session value so logs and incident
// scanners can spot one. Tokens use `ab_*`; sessions use `abs_*`. The
// "abs" reads as "AgentBoard session".
const SessionPrefix = "abs_"

// DefaultSessionTTL is what CreateSession applies when expiresIn is
// zero. 30 days mirrors typical browser-session expectations: long
// enough that users don't re-auth daily, short enough that a stolen
// laptop loses access in a calendar month.
const DefaultSessionTTL = 30 * 24 * time.Hour

// Standard session errors. Every "wrong" outcome maps to one of
// these; handlers translate them to 401.
var (
	ErrSessionInvalid = errors.New("session invalid")
	ErrSessionExpired = errors.New("session expired")
	ErrSessionRevoked = errors.New("session revoked")
)

// Session is one row in user_sessions. Plaintext lives only in the
// CreateSession return value and on the client cookie; the DB stores
// only sha256(plaintext).
type Session struct {
	ID         string     `json:"id"`
	Username   string     `json:"username"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	UserAgent  string     `json:"user_agent,omitempty"`
	IP         string     `json:"ip,omitempty"`
}

// HashSession is the one-way hash stored in user_sessions. Like
// HashToken: sessions are random high-entropy strings, not passwords.
// sha256 gives constant-time equality + replay-safe storage; argon2id
// would only add latency on the hot path with no security benefit.
func HashSession(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// generateSessionPlaintext returns the user-facing session value
// shaped as `abs_<43 base64 chars>`. 32 bytes of entropy; mirrors the
// PAT generator's choices so secret-scanners that already know about
// `ab_*` will surface leaked sessions too.
func generateSessionPlaintext() (string, error) {
	var raw [32]byte
	if _, err := readRandom(raw[:]); err != nil {
		return "", err
	}
	return SessionPrefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// readRandom is split out so tests can override it. Defined as a var
// for that reason.
var readRandom = func(b []byte) (int, error) {
	return rand.Read(b)
}

// SetPassword stores a fresh argon2id hash for the user. Used by:
//   - the admin CLI (`agentboard admin set-password`),
//   - the self-or-admin /api/users/{u}/password endpoint,
//   - the invitation redeem path when the redeemer chose a password.
//
// The plaintext never reaches the DB or any logger; only the hash
// does. password_updated_at is bumped to NOW so the UI can surface
// "last changed N days ago".
func (s *Store) SetPassword(username, plain string) error {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return ErrNotFound
	}
	hash, err := HashPassword(plain)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Unix()
	res, err := s.db.Exec(
		`UPDATE users SET password_hash = ?, password_updated_at = ?
		 WHERE username = ? COLLATE NOCASE AND deactivated_at IS NULL`,
		hash, now, username,
	)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearPassword removes the password from a user. Used by the admin
// surface ("revoke browser login for this user"). Does NOT touch
// sessions — call RevokeAllSessionsForUser for that.
func (s *Store) ClearPassword(username string) error {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return ErrNotFound
	}
	_, err := s.db.Exec(
		`UPDATE users SET password_hash = NULL, password_updated_at = NULL
		 WHERE username = ? COLLATE NOCASE`,
		username,
	)
	return err
}

// VerifyLogin is the password-based authentication entry point. The
// only public surface that turns (username, password) into a User
// pointer. Constant-time on both branches: lookups always run argon2
// over a placeholder hash if the user is missing, so a wrong-username
// attempt and a wrong-password attempt take the same wall time.
//
// Returns ErrNotFound for both "no such user" and "wrong password" —
// the API surface should use the same shape too. Deactivated users
// also surface ErrNotFound (no signal that the username ever existed).
//
// PasswordSet=false (no `password_hash` row, opt-in soft-launch case)
// counts as "no such login": the user exists but hasn't set a
// password yet, so this code path can't authenticate them.
func (s *Store) VerifyLogin(username, plain string) (*User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	row := s.db.QueryRow(
		`SELECT password_hash FROM users
		 WHERE username = ? COLLATE NOCASE AND deactivated_at IS NULL`,
		username,
	)
	var stored sql.NullString
	err := row.Scan(&stored)
	// We always run an argon2 verify — a real one if we have a hash,
	// a dummy one if we don't — so the wall time of the wrong-username
	// path matches the wrong-password path.
	hashStr := stored.String
	if !stored.Valid || hashStr == "" {
		hashStr = dummyArgonHash
	}
	matches := VerifyPassword(plain, hashStr)
	if errors.Is(err, sql.ErrNoRows) || !stored.Valid || hashStr == dummyArgonHash {
		// Either the user doesn't exist, or has no password set.
		// Run the dummy compare to keep timing flat then return.
		_ = matches
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !matches {
		return nil, ErrNotFound
	}
	return s.GetUser(username)
}

// dummyArgonHash is the stand-in we VerifyPassword against on the
// "no such user / no password set" branch so wrong-username takes
// roughly the same wall time as wrong-password. Generated with:
//
//	hash, _ := HashPassword("dummy-static-not-a-real-password-1234")
//
// Pre-computed at init time so tests don't pay the argon cost.
var dummyArgonHash string

func init() {
	// Build a real-shape argon2id hash without involving the package's
	// own MinPasswordLen check (the input is fine, but we want to
	// avoid coupling at init-time).
	hash, err := HashPassword("dummy-static-not-a-real-password-1234")
	if err == nil {
		dummyArgonHash = hash
	}
}

// CreateSession mints a new session row. Returns (sessionID,
// plaintextSession). The plaintext is shown ONCE — caller sets it as
// a cookie, never persists it on the server.
//
// expiresIn=0 → DefaultSessionTTL.
func (s *Store) CreateSession(username, userAgent, ip string, expiresIn time.Duration) (sessionID, plaintext string, err error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return "", "", ErrNotFound
	}
	if expiresIn <= 0 {
		expiresIn = DefaultSessionTTL
	}
	plain, err := generateSessionPlaintext()
	if err != nil {
		return "", "", err
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	expires := now.Add(expiresIn)
	_, err = s.db.Exec(
		`INSERT INTO user_sessions (id, session_hash, username, created_at, expires_at, user_agent, ip)
		 VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))`,
		id, HashSession(plain), username, now.Unix(), expires.Unix(), userAgent, ip,
	)
	if err != nil {
		return "", "", fmt.Errorf("insert session: %w", err)
	}
	return id, plain, nil
}

// ResolveSession takes the plaintext session value (from the cookie)
// and returns (user, session). Errors map cleanly:
//   - ErrSessionInvalid: no matching row, or hash mismatch
//   - ErrSessionRevoked: row exists but revoked_at is set
//   - ErrSessionExpired: row exists but expires_at < now
//   - ErrUserDeactivated: row + user exist, but the user is deactivated
//
// In every error case the *Session and *User returned reflect what we
// found (so callers can log) — except ErrSessionInvalid, where we
// have nothing.
func (s *Store) ResolveSession(plaintext string) (*User, *Session, error) {
	if plaintext == "" {
		return nil, nil, ErrSessionInvalid
	}
	row := s.db.QueryRow(
		`SELECT s.id, s.username, s.created_at, s.last_used_at, s.expires_at, s.revoked_at, s.user_agent, s.ip,
		        u.deactivated_at
		 FROM user_sessions s JOIN users u ON u.username = s.username COLLATE NOCASE
		 WHERE s.session_hash = ?`,
		HashSession(plaintext),
	)
	var sess Session
	var (
		created     int64
		lastUsed    sql.NullInt64
		expires     int64
		revoked     sql.NullInt64
		ua          sql.NullString
		ip          sql.NullString
		deactivated sql.NullInt64
	)
	if err := row.Scan(&sess.ID, &sess.Username, &created, &lastUsed, &expires, &revoked, &ua, &ip, &deactivated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrSessionInvalid
		}
		return nil, nil, err
	}
	sess.CreatedAt = time.Unix(created, 0).UTC()
	sess.ExpiresAt = time.Unix(expires, 0).UTC()
	sess.UserAgent = ua.String
	sess.IP = ip.String
	if lastUsed.Valid {
		t := time.Unix(lastUsed.Int64, 0).UTC()
		sess.LastUsedAt = &t
	}
	if revoked.Valid {
		t := time.Unix(revoked.Int64, 0).UTC()
		sess.RevokedAt = &t
		return nil, &sess, ErrSessionRevoked
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		return nil, &sess, ErrSessionExpired
	}
	user, err := s.GetUser(sess.Username)
	if err != nil {
		return nil, &sess, err
	}
	if deactivated.Valid {
		return user, &sess, ErrUserDeactivated
	}
	return user, &sess, nil
}

// TouchSessionLastUsed bumps last_used_at on a session. Called by
// middleware via the same coalesced path as TouchTokenLastUsed.
func (s *Store) TouchSessionLastUsed(id string) error {
	_, err := s.db.Exec(
		`UPDATE user_sessions SET last_used_at = ? WHERE id = ?`,
		time.Now().UTC().Unix(), id,
	)
	return err
}

// RevokeSession soft-deletes a single session by id.
func (s *Store) RevokeSession(id string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.Exec(
		`UPDATE user_sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
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

// RevokeAllSessionsForUser is the lockout-recovery hammer. Used by
// the admin CLI and by ClearPassword's caller when an admin wants
// to nuke browser access to a user.
func (s *Store) RevokeAllSessionsForUser(username string) (int, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return 0, ErrNotFound
	}
	now := time.Now().UTC().Unix()
	res, err := s.db.Exec(
		`UPDATE user_sessions SET revoked_at = COALESCE(revoked_at, ?)
		 WHERE username = ? COLLATE NOCASE AND revoked_at IS NULL`,
		now, username,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ListSessionsForUser returns every session row, active and not.
// Used by the per-user "manage sessions" surface. Plaintext is never
// returned — that's only known at create time.
func (s *Store) ListSessionsForUser(username string) ([]Session, error) {
	rows, err := s.db.Query(
		`SELECT id, username, created_at, last_used_at, expires_at, revoked_at, user_agent, ip
		 FROM user_sessions WHERE username = ? COLLATE NOCASE
		 ORDER BY created_at DESC`,
		strings.ToLower(username),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		var (
			created  int64
			lastUsed sql.NullInt64
			expires  int64
			revoked  sql.NullInt64
			ua       sql.NullString
			ip       sql.NullString
		)
		if err := rows.Scan(&sess.ID, &sess.Username, &created, &lastUsed, &expires, &revoked, &ua, &ip); err != nil {
			return nil, err
		}
		sess.CreatedAt = time.Unix(created, 0).UTC()
		sess.ExpiresAt = time.Unix(expires, 0).UTC()
		sess.UserAgent = ua.String
		sess.IP = ip.String
		if lastUsed.Valid {
			t := time.Unix(lastUsed.Int64, 0).UTC()
			sess.LastUsedAt = &t
		}
		if revoked.Valid {
			t := time.Unix(revoked.Int64, 0).UTC()
			sess.RevokedAt = &t
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// GenerateCSRFToken returns a fresh opaque CSRF value. Cookie + form
// share the same string — the middleware enforces equality, no
// crypto on this path.
func GenerateCSRFToken() (string, error) {
	var raw [16]byte
	if _, err := readRandom(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// ConstantTimeStringEqual is the helper handlers use for double-submit
// CSRF compare. Wraps subtle.ConstantTimeCompare so callers don't need
// to import crypto/subtle.
func ConstantTimeStringEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
