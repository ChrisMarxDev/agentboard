// Package webhooks implements outbound event delivery for AgentBoard.
// Subscribers register an `(event_pattern, destination_url, secret)`
// triple; when a matching event fires on the broadcaster, the delivery
// worker POSTs a signed payload to the destination and records the
// attempt. HMAC-SHA256 over the request body gives receivers a way to
// verify authenticity without shared TLS infrastructure.
//
// Write-path invariant: webhooks only *observe* state changes — they
// never mutate anything in AgentBoard. A webhook failing delivery
// cannot block the underlying write.
package webhooks

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// SecretPrefix tags subscription secrets visually — `wh_` distinct
	// from `ab_` (agent tokens) and `sh_` (share tokens).
	SecretPrefix = "wh_"
	// SecretRandBytes controls how long the secret is.
	SecretRandBytes = 32
)

var (
	ErrNotFound  = errors.New("webhooks: subscription not found")
	ErrRevoked   = errors.New("webhooks: subscription revoked")
	ErrInvalidID = errors.New("webhooks: invalid id")
)

// DeliveryStatus reflects the last attempt to POST to the destination.
type DeliveryStatus string

const (
	StatusPending      DeliveryStatus = "pending"       // never attempted
	StatusOK           DeliveryStatus = "ok"            // 2xx
	StatusRetrying     DeliveryStatus = "retrying"      // non-2xx, still has attempts left
	StatusDeadLettered DeliveryStatus = "dead_lettered" // gave up
)

// Subscription is the agent-visible record. The secret hash stays
// server-side; plaintext is returned exactly once on create.
type Subscription struct {
	ID             string         `json:"id"`
	EventPattern   string         `json:"event_pattern"`
	DestinationURL string         `json:"destination_url"`
	Label          string         `json:"label,omitempty"`
	CreatedBy      string         `json:"created_by"`
	CreatedAt      time.Time      `json:"created_at"`
	RevokedAt      *time.Time     `json:"revoked_at,omitempty"`
	LastAttemptAt  *time.Time     `json:"last_attempt_at,omitempty"`
	LastSuccessAt  *time.Time     `json:"last_success_at,omitempty"`
	LastStatus     DeliveryStatus `json:"last_status"`
	LastStatusCode int            `json:"last_status_code,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	FailureCount   int            `json:"failure_count"`
	SuccessCount   int            `json:"success_count"`
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id                 TEXT NOT NULL PRIMARY KEY,
    event_pattern      TEXT NOT NULL,
    destination_url    TEXT NOT NULL,
    secret_hash        TEXT NOT NULL,
    label              TEXT,
    created_by         TEXT NOT NULL,
    created_at         INTEGER NOT NULL,
    revoked_at         INTEGER,
    last_attempt_at    INTEGER,
    last_success_at    INTEGER,
    last_status        TEXT NOT NULL DEFAULT 'pending',
    last_status_code   INTEGER NOT NULL DEFAULT 0,
    last_error         TEXT,
    failure_count      INTEGER NOT NULL DEFAULT 0,
    success_count      INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE INDEX IF NOT EXISTS idx_webhook_subs_active
    ON webhook_subscriptions(event_pattern) WHERE revoked_at IS NULL;
`

// Store manages webhook_subscriptions rows.
type Store struct {
	db *sql.DB
}

// NewStore creates (and migrates) the store.
func NewStore(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("webhooks: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// GenerateSecret mints a fresh `wh_<64 hex>` secret.
func GenerateSecret() (string, error) {
	var buf [SecretRandBytes]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return SecretPrefix + hex.EncodeToString(buf[:]), nil
}

// HashSecret returns the hex-encoded SHA256 of the plaintext secret.
func HashSecret(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// CreateParams captures user-authored fields for a new subscription.
// Secret is optional; if empty the server mints one and returns the
// plaintext in the Create response.
type CreateParams struct {
	EventPattern   string
	DestinationURL string
	Label          string
	CreatedBy      string
	Secret         string // plaintext; optional
}

// Create inserts a new subscription. Returns (plaintext secret, row).
// The plaintext is returned ONCE — the caller must surface it to the
// user immediately.
func (s *Store) Create(p CreateParams) (string, *Subscription, error) {
	if strings.TrimSpace(p.EventPattern) == "" {
		return "", nil, errors.New("webhooks: event_pattern required")
	}
	if strings.TrimSpace(p.DestinationURL) == "" {
		return "", nil, errors.New("webhooks: destination_url required")
	}
	if strings.TrimSpace(p.CreatedBy) == "" {
		return "", nil, errors.New("webhooks: created_by required")
	}
	secret := p.Secret
	if secret == "" {
		var err error
		secret, err = GenerateSecret()
		if err != nil {
			return "", nil, err
		}
	}
	now := time.Now().UTC()
	id := HashSecret(secret)[:16]
	_, err := s.db.Exec(`
		INSERT INTO webhook_subscriptions (
			id, event_pattern, destination_url, secret_hash, label,
			created_by, created_at, last_status, last_status_code, failure_count, success_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 0, 0, 0)`,
		id, p.EventPattern, p.DestinationURL, HashSecret(secret),
		nullableString(p.Label), p.CreatedBy, now.Unix(),
	)
	if err != nil {
		return "", nil, fmt.Errorf("webhooks: insert: %w", err)
	}
	return secret, &Subscription{
		ID:             id,
		EventPattern:   p.EventPattern,
		DestinationURL: p.DestinationURL,
		Label:          p.Label,
		CreatedBy:      p.CreatedBy,
		CreatedAt:      now,
		LastStatus:     StatusPending,
	}, nil
}

// Get fetches a subscription by ID.
func (s *Store) Get(id string) (*Subscription, error) {
	row := s.db.QueryRow(`
		SELECT id, event_pattern, destination_url, label, created_by, created_at,
		       revoked_at, last_attempt_at, last_success_at, last_status,
		       last_status_code, last_error, failure_count, success_count
		FROM webhook_subscriptions WHERE id = ?`, id)
	sub, err := scanSubscription(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sub, err
}

// ListActive returns every non-revoked subscription, sorted by
// creation date descending.
func (s *Store) ListActive() ([]*Subscription, error) {
	return s.listWhere(`WHERE revoked_at IS NULL`)
}

// ListAll returns everything including revoked rows (admin view).
func (s *Store) ListAll() ([]*Subscription, error) {
	return s.listWhere(``)
}

func (s *Store) listWhere(whereClause string) ([]*Subscription, error) {
	rows, err := s.db.Query(`
		SELECT id, event_pattern, destination_url, label, created_by, created_at,
		       revoked_at, last_attempt_at, last_success_at, last_status,
		       last_status_code, last_error, failure_count, success_count
		FROM webhook_subscriptions ` + whereClause + ` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Subscription
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// Revoke marks a subscription as revoked. Idempotent.
func (s *Store) Revoke(id string) error {
	res, err := s.db.Exec(`
		UPDATE webhook_subscriptions
		SET revoked_at = COALESCE(revoked_at, ?)
		WHERE id = ?`, time.Now().UTC().Unix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateParams captures in-place edits. Fields with nil pointers are
// left unchanged.
type UpdateParams struct {
	EventPattern   *string
	DestinationURL *string
	Label          *string
}

// Update applies a partial update to an unrevoked subscription.
func (s *Store) Update(id string, p UpdateParams) (*Subscription, error) {
	sub, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if sub.RevokedAt != nil {
		return nil, ErrRevoked
	}
	if p.EventPattern != nil {
		sub.EventPattern = *p.EventPattern
	}
	if p.DestinationURL != nil {
		sub.DestinationURL = *p.DestinationURL
	}
	if p.Label != nil {
		sub.Label = *p.Label
	}
	_, err = s.db.Exec(`
		UPDATE webhook_subscriptions
		SET event_pattern = ?, destination_url = ?, label = ?
		WHERE id = ?`,
		sub.EventPattern, sub.DestinationURL, nullableString(sub.Label), id,
	)
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// RecordAttempt updates delivery bookkeeping after a POST attempt.
// status ∈ {"ok","retrying","dead_lettered"}; errMsg is empty on ok.
func (s *Store) RecordAttempt(id string, status DeliveryStatus, code int, errMsg string, success bool) error {
	now := time.Now().UTC().Unix()
	incSuccess := 0
	incFail := 0
	if success {
		incSuccess = 1
	} else {
		incFail = 1
	}
	var successAtCol any
	if success {
		successAtCol = now
	}
	_, err := s.db.Exec(`
		UPDATE webhook_subscriptions SET
			last_attempt_at = ?,
			last_success_at = COALESCE(?, last_success_at),
			last_status = ?,
			last_status_code = ?,
			last_error = ?,
			failure_count = failure_count + ?,
			success_count = success_count + ?
		WHERE id = ?`,
		now, successAtCol, string(status), code, nullableString(errMsg),
		incFail, incSuccess, id,
	)
	return err
}

// scanner wraps *sql.Row / *sql.Rows for our shared scan path.
type scanner interface {
	Scan(dest ...any) error
}

func scanSubscription(sc scanner) (*Subscription, error) {
	var (
		sub            Subscription
		label          sql.NullString
		createdAt      int64
		revokedAt      sql.NullInt64
		lastAttemptAt  sql.NullInt64
		lastSuccessAt  sql.NullInt64
		lastStatus     string
		lastStatusCode int
		lastError      sql.NullString
	)
	err := sc.Scan(
		&sub.ID, &sub.EventPattern, &sub.DestinationURL, &label,
		&sub.CreatedBy, &createdAt, &revokedAt, &lastAttemptAt, &lastSuccessAt,
		&lastStatus, &lastStatusCode, &lastError,
		&sub.FailureCount, &sub.SuccessCount,
	)
	if err != nil {
		return nil, err
	}
	sub.CreatedAt = time.Unix(createdAt, 0).UTC()
	if label.Valid {
		sub.Label = label.String
	}
	if revokedAt.Valid {
		t := time.Unix(revokedAt.Int64, 0).UTC()
		sub.RevokedAt = &t
	}
	if lastAttemptAt.Valid {
		t := time.Unix(lastAttemptAt.Int64, 0).UTC()
		sub.LastAttemptAt = &t
	}
	if lastSuccessAt.Valid {
		t := time.Unix(lastSuccessAt.Int64, 0).UTC()
		sub.LastSuccessAt = &t
	}
	sub.LastStatus = DeliveryStatus(lastStatus)
	sub.LastStatusCode = lastStatusCode
	if lastError.Valid {
		sub.LastError = lastError.String
	}
	return &sub, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ------------------------------------------------------------------
// Event matching
// ------------------------------------------------------------------

// MatchEvent reports whether a webhook pattern matches an event name.
//
// Semantics (intentionally minimal for v0):
//   - "*" matches everything
//   - "foo" matches exactly "foo"
//   - "foo.*" matches "foo.anything" (one or more segments)
//   - "foo.bar.*" matches "foo.bar.X" and deeper
//   - Leading/trailing whitespace is ignored
//
// We keep this deliberately simple. A richer glob matcher can land
// with Webhook UI when operators start writing patterns by hand.
func MatchEvent(pattern, event string) bool {
	pattern = strings.TrimSpace(pattern)
	event = strings.TrimSpace(event)
	if pattern == "" || event == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if pattern == event {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := pattern[:len(pattern)-1] // drop the "*", keep the trailing "."
		return strings.HasPrefix(event, prefix)
	}
	return false
}
