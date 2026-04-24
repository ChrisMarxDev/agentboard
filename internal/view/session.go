package view

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// SessionCookieName is the cookie the SPA carries after redeeming a
// share token. Read-only; HttpOnly + SameSite=Strict on the write side.
const SessionCookieName = "ab_view_session"

// DefaultSessionTTL is how long a redeemed share-cookie stays valid.
// Shorter than the underlying share token so the plaintext token gets
// out of logs fast and the cookie rotates.
const DefaultSessionTTL = 1 * time.Hour

var (
	ErrSessionNotFound = errors.New("view: session not found")
	ErrSessionExpired  = errors.New("view: session expired")
)

// Session is a redeemed share token cookie-backed. Auth'd users do NOT
// use these; they rely on their bearer for every request.
type Session struct {
	ID           string
	CookieHash   string
	ShareTokenID string
	Path         string // the share's scoped page path
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

// SessionStore persists view_sessions rows and verifies cookies.
type SessionStore struct {
	db *sql.DB
}

// NewSessionStore opens (and migrates) the view_sessions table.
func NewSessionStore(db *sql.DB) (*SessionStore, error) {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS view_sessions (
		id              TEXT PRIMARY KEY,
		cookie_hash     TEXT NOT NULL UNIQUE,
		share_token_id  TEXT NOT NULL,
		path            TEXT NOT NULL,
		expires_at      INTEGER NOT NULL,
		created_at      INTEGER NOT NULL,
		FOREIGN KEY(share_token_id) REFERENCES share_tokens(id) ON DELETE CASCADE
	) STRICT;
	CREATE INDEX IF NOT EXISTS idx_view_sessions_cookie ON view_sessions(cookie_hash);
	CREATE INDEX IF NOT EXISTS idx_view_sessions_share ON view_sessions(share_token_id);
	`)
	if err != nil {
		return nil, fmt.Errorf("view: migrate sessions: %w", err)
	}
	return &SessionStore{db: db}, nil
}

// Create mints a fresh session and returns (plaintextCookie, session).
func (s *SessionStore) Create(shareTokenID, path string, ttl time.Duration) (string, *Session, error) {
	// Only zero TTL falls back to the default; negative values are
	// honoured so tests can mint pre-expired sessions.
	if ttl == 0 {
		ttl = DefaultSessionTTL
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", nil, err
	}
	plaintext := hex.EncodeToString(raw[:])
	hash := hashCookie(plaintext)
	id := hash[:16]
	now := time.Now().UTC()
	exp := now.Add(ttl)
	_, err := s.db.Exec(
		`INSERT INTO view_sessions (id, cookie_hash, share_token_id, path, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, hash, shareTokenID, path, exp.Unix(), now.Unix(),
	)
	if err != nil {
		return "", nil, err
	}
	return plaintext, &Session{
		ID: id, CookieHash: hash, ShareTokenID: shareTokenID,
		Path: path, ExpiresAt: exp, CreatedAt: now,
	}, nil
}

// Resolve looks up a cookie and returns its session if valid.
func (s *SessionStore) Resolve(cookiePlaintext string) (*Session, error) {
	if cookiePlaintext == "" {
		return nil, ErrSessionNotFound
	}
	hash := hashCookie(cookiePlaintext)
	var sess Session
	var exp, created int64
	err := s.db.QueryRow(
		`SELECT id, cookie_hash, share_token_id, path, expires_at, created_at
		 FROM view_sessions WHERE cookie_hash = ?`, hash,
	).Scan(&sess.ID, &sess.CookieHash, &sess.ShareTokenID, &sess.Path, &exp, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.ExpiresAt = time.Unix(exp, 0).UTC()
	sess.CreatedAt = time.Unix(created, 0).UTC()
	if time.Now().UTC().After(sess.ExpiresAt) {
		return nil, ErrSessionExpired
	}
	return &sess, nil
}

// Delete removes a session by ID (used on logout/revoke-cascade).
func (s *SessionStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM view_sessions WHERE id = ?`, id)
	return err
}

// DeleteByShareToken removes all sessions for a revoked share token.
func (s *SessionStore) DeleteByShareToken(shareTokenID string) error {
	_, err := s.db.Exec(`DELETE FROM view_sessions WHERE share_token_id = ?`, shareTokenID)
	return err
}

// PurgeExpired drops rows past their expiry. Called periodically.
func (s *SessionStore) PurgeExpired() error {
	_, err := s.db.Exec(`DELETE FROM view_sessions WHERE expires_at < ?`, time.Now().UTC().Unix())
	return err
}

func hashCookie(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}
