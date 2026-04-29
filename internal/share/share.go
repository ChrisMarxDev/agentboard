// Package share implements single-page public share tokens.
//
// A share token grants anonymous READ access to one specific content
// path. It NEVER grants write access, regardless of what the request
// looks like — writes always route through the regular auth layer.
//
// Tokens are presented via either the `X-Share-Token: sh_<...>` header
// or the `?share=sh_<...>` query parameter. Only tokens that match the
// current request path are honoured; a token scoped to
// `/handbook/onboarding` cannot be used to read `/api/admin/users`.
package share

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// TokenPrefix marks every share token so operators can tell them
	// apart from agent tokens (`ab_...`) at a glance.
	TokenPrefix = "sh_"
	// TokenRandBytes is the size of the random component; hex-encoded
	// that's 64 chars, plus the prefix.
	TokenRandBytes = 32
)

var (
	ErrNotFound  = errors.New("share: token not found")
	ErrRevoked   = errors.New("share: token revoked")
	ErrExpired   = errors.New("share: token expired")
	ErrWrongPath = errors.New("share: token scoped to a different path")
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS share_tokens (
    id             TEXT NOT NULL PRIMARY KEY,
    path           TEXT NOT NULL,
    token_hash     TEXT NOT NULL UNIQUE,
    created_by     TEXT NOT NULL,
    created_at     INTEGER NOT NULL,
    expires_at     INTEGER,
    revoked_at     INTEGER,
    last_used_at   INTEGER,
    use_count      INTEGER NOT NULL DEFAULT 0,
    label          TEXT,
    max_uses       INTEGER
) STRICT;

CREATE INDEX IF NOT EXISTS idx_share_tokens_path ON share_tokens(path);
CREATE INDEX IF NOT EXISTS idx_share_tokens_active ON share_tokens(token_hash) WHERE revoked_at IS NULL;
`

// migrations adds columns to an existing table if they're missing —
// needed because the original schema omitted label/max_uses. SQLite
// doesn't support `ADD COLUMN IF NOT EXISTS`, so we check.
func applySchemaUpgrades(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(share_tokens)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var deflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &deflt, &pk); err != nil {
			return err
		}
		have[name] = true
	}
	if !have["label"] {
		if _, err := db.Exec(`ALTER TABLE share_tokens ADD COLUMN label TEXT`); err != nil {
			return err
		}
	}
	if !have["max_uses"] {
		if _, err := db.Exec(`ALTER TABLE share_tokens ADD COLUMN max_uses INTEGER`); err != nil {
			return err
		}
	}
	return nil
}

// Token is the public-facing record. token_hash stays server-side.
type Token struct {
	ID         string
	Path       string
	CreatedBy  string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	LastUsedAt *time.Time
	UseCount   int
	// Label is the operator-authored caption ("Q1 client review")
	// surfaced on the admin shares screen. Optional.
	Label string
	// MaxUses caps the total number of redemptions. NULL/zero in the
	// column means unlimited; a positive integer means the token
	// stops working once use_count reaches it.
	MaxUses *int
}

// Store manages share_tokens rows.
type Store struct {
	db *sql.DB
}

// NewStore creates (and migrates) the share store over an existing
// *sql.DB. Safe to call repeatedly; the CREATE statements are guarded
// with IF NOT EXISTS.
func NewStore(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("share: migrate: %w", err)
	}
	if err := applySchemaUpgrades(db); err != nil {
		return nil, fmt.Errorf("share: upgrade: %w", err)
	}
	return &Store{db: db}, nil
}

// Generate produces a new `sh_<64 hex>` token. The caller stores the
// hash, shows the plaintext once, then discards it.
func Generate() (string, error) {
	var buf [TokenRandBytes]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return TokenPrefix + hex.EncodeToString(buf[:]), nil
}

// HashToken returns the hex-encoded SHA256 of the plaintext token.
// Constant-time compare at lookup is the caller's job; we use `UNIQUE`
// on token_hash so the DB itself enforces at-most-one.
func HashToken(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// constantTimeEqHex compares two hex strings without leaking length
// via early-exit. Used at verification time.
func constantTimeEqHex(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// CreateParams captures the fields needed to issue a new token.
type CreateParams struct {
	Path      string
	CreatedBy string
	TTL       time.Duration // 0 = never expires
	Label     string        // optional human-readable caption
	MaxUses   int           // 0 = unlimited; N = stop working after N redeems
}

// Create issues a new token and returns (plaintext, record). The
// plaintext is ONLY returned once — the caller must surface it to the
// user at this call-site.
func (s *Store) Create(p CreateParams) (string, *Token, error) {
	if strings.TrimSpace(p.Path) == "" {
		return "", nil, errors.New("share: path required")
	}
	if strings.TrimSpace(p.CreatedBy) == "" {
		return "", nil, errors.New("share: created_by required")
	}
	plaintext, err := Generate()
	if err != nil {
		return "", nil, err
	}
	id := HashToken(plaintext)[:16] // short, stable, safe to show
	now := time.Now().UTC()
	var expiresAt sql.NullInt64
	var expiresPtr *time.Time
	if p.TTL != 0 {
		// Nonzero TTL → record an expiry. Negative TTL produces an
		// already-expired token (useful for tests).
		t := now.Add(p.TTL)
		expiresAt = sql.NullInt64{Int64: t.Unix(), Valid: true}
		expiresPtr = &t
	}
	var labelCol sql.NullString
	if p.Label != "" {
		labelCol = sql.NullString{String: p.Label, Valid: true}
	}
	var maxUsesCol sql.NullInt64
	var maxUsesPtr *int
	if p.MaxUses > 0 {
		maxUsesCol = sql.NullInt64{Int64: int64(p.MaxUses), Valid: true}
		mu := p.MaxUses
		maxUsesPtr = &mu
	}
	_, err = s.db.Exec(`
		INSERT INTO share_tokens (id, path, token_hash, created_by, created_at, expires_at, label, max_uses)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.Path, HashToken(plaintext), p.CreatedBy, now.Unix(), expiresAt, labelCol, maxUsesCol,
	)
	if err != nil {
		return "", nil, fmt.Errorf("share: insert: %w", err)
	}
	return plaintext, &Token{
		ID:        id,
		Path:      p.Path,
		CreatedBy: p.CreatedBy,
		CreatedAt: now,
		ExpiresAt: expiresPtr,
		Label:     p.Label,
		MaxUses:   maxUsesPtr,
	}, nil
}

// Resolve looks up a plaintext token and returns the record if it's
// valid — not revoked, not expired. Call before authorising a request.
// Also bumps last_used_at / use_count (best effort).
func (s *Store) Resolve(plaintext string) (*Token, error) {
	if !strings.HasPrefix(plaintext, TokenPrefix) {
		return nil, ErrNotFound
	}
	hash := HashToken(plaintext)
	var (
		id         string
		path       string
		storedHash string
		createdBy  string
		createdAt  int64
		expiresAt  sql.NullInt64
		revokedAt  sql.NullInt64
		lastUsedAt sql.NullInt64
		useCount   int
		label      sql.NullString
		maxUses    sql.NullInt64
	)
	err := s.db.QueryRow(`
		SELECT id, path, token_hash, created_by, created_at, expires_at, revoked_at, last_used_at, use_count, label, max_uses
		FROM share_tokens WHERE token_hash = ?`, hash).Scan(
		&id, &path, &storedHash, &createdBy, &createdAt, &expiresAt, &revokedAt, &lastUsedAt, &useCount, &label, &maxUses,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// Defensive constant-time compare — the UNIQUE index already
	// guarantees a single match, but timing-side-channel-wise we
	// prefer an explicit check.
	if !constantTimeEqHex(storedHash, hash) {
		return nil, ErrNotFound
	}
	if revokedAt.Valid {
		return nil, ErrRevoked
	}
	// max_uses enforcement — refuse after N redeems. Useful for
	// single-use preview links.
	if maxUses.Valid && useCount >= int(maxUses.Int64) {
		return nil, ErrExpired
	}
	tok := &Token{
		ID:        id,
		Path:      path,
		CreatedBy: createdBy,
		CreatedAt: time.Unix(createdAt, 0).UTC(),
		UseCount:  useCount,
	}
	if label.Valid {
		tok.Label = label.String
	}
	if maxUses.Valid {
		n := int(maxUses.Int64)
		tok.MaxUses = &n
	}
	if expiresAt.Valid {
		t := time.Unix(expiresAt.Int64, 0).UTC()
		tok.ExpiresAt = &t
		if time.Now().UTC().After(t) {
			return nil, ErrExpired
		}
	}
	if lastUsedAt.Valid {
		t := time.Unix(lastUsedAt.Int64, 0).UTC()
		tok.LastUsedAt = &t
	}
	// Best-effort bump of last_used_at + use_count. Swallow errors —
	// failing to update usage shouldn't 500 a valid share read.
	_, _ = s.db.Exec(`UPDATE share_tokens SET last_used_at = ?, use_count = use_count + 1 WHERE id = ?`,
		time.Now().UTC().Unix(), id)
	return tok, nil
}

// ListForPath returns every unrevoked token that still grants access to
// `path`. Expired tokens are included so the UI can show "expired"
// badges rather than making them vanish.
func (s *Store) ListForPath(path string) ([]*Token, error) {
	return s.listWhere(`WHERE path = ? AND revoked_at IS NULL`, path)
}

// ListAll returns every unrevoked token across the whole instance.
// Used by the admin shares screen.
func (s *Store) ListAll() ([]*Token, error) {
	return s.listWhere(`WHERE revoked_at IS NULL`)
}

func (s *Store) listWhere(whereClause string, args ...any) ([]*Token, error) {
	rows, err := s.db.Query(`
		SELECT id, path, created_by, created_at, expires_at, last_used_at, use_count, label, max_uses
		FROM share_tokens `+whereClause+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Token
	for rows.Next() {
		var (
			tok        Token
			createdAt  int64
			expiresAt  sql.NullInt64
			lastUsedAt sql.NullInt64
			label      sql.NullString
			maxUses    sql.NullInt64
		)
		if err := rows.Scan(&tok.ID, &tok.Path, &tok.CreatedBy, &createdAt, &expiresAt, &lastUsedAt, &tok.UseCount, &label, &maxUses); err != nil {
			return nil, err
		}
		tok.CreatedAt = time.Unix(createdAt, 0).UTC()
		if expiresAt.Valid {
			t := time.Unix(expiresAt.Int64, 0).UTC()
			tok.ExpiresAt = &t
		}
		if lastUsedAt.Valid {
			t := time.Unix(lastUsedAt.Int64, 0).UTC()
			tok.LastUsedAt = &t
		}
		if label.Valid {
			tok.Label = label.String
		}
		if maxUses.Valid {
			n := int(maxUses.Int64)
			tok.MaxUses = &n
		}
		out = append(out, &tok)
	}
	return out, rows.Err()
}

// Revoke marks a token as revoked. Idempotent — revoking an already
// revoked token is a no-op and returns ErrNotFound only if the ID
// doesn't exist.
func (s *Store) Revoke(id string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.Exec(`UPDATE share_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
