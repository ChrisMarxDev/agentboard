// Package inbox implements per-user reminder queues. A mention, a
// kanban assignment, an approval nudge, a dead-lettered webhook —
// each creates an inbox item for its recipient. Users click through
// to read / archive / delete. Retention defaults to 60 days; purge
// is opportunistic on read.
//
// Inbox items are produced by the server on write-path detection
// (`@mentions` in page source, `assignees` diffs on array rows, etc.)
// and consumed by the user via REST. No SSE push in v0 — clients poll
// the count endpoint.
package inbox

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kind enumerates inbox item types. Keep this tight so clients can
// exhaustively switch on it in UI rendering.
type Kind string

const (
	KindMention         Kind = "mention"
	KindAssignment      Kind = "assignment"
	KindApprovalRequest Kind = "approval_request"
	KindWebhookFailure  Kind = "webhook_failure"
)

// Item is the row shape. Fields map to the page-page spec verbatim.
type Item struct {
	ID          int64      `json:"id"`
	Recipient   string     `json:"recipient"`
	Kind        Kind       `json:"kind"`
	SubjectPath string     `json:"subject_path,omitempty"`
	SubjectRef  string     `json:"subject_ref,omitempty"`
	Title       string     `json:"title"`
	Actor       string     `json:"actor,omitempty"`
	At          time.Time  `json:"at"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
}

var (
	ErrNotFound  = errors.New("inbox: not found")
	ErrForbidden = errors.New("inbox: not the recipient")
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS inbox_items (
    id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    recipient    TEXT NOT NULL,
    kind         TEXT NOT NULL,
    subject_path TEXT,
    subject_ref  TEXT,
    title        TEXT NOT NULL,
    actor        TEXT,
    at           INTEGER NOT NULL,
    read_at      INTEGER,
    archived_at  INTEGER
) STRICT;

CREATE INDEX IF NOT EXISTS idx_inbox_recipient_unread
    ON inbox_items(recipient, read_at);
CREATE INDEX IF NOT EXISTS idx_inbox_recipient_at
    ON inbox_items(recipient, at DESC);
`

// Store persists inbox rows.
type Store struct {
	db *sql.DB
}

// NewStore creates (and migrates) the store.
func NewStore(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("inbox: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// CreateParams captures the fields a producer supplies.
type CreateParams struct {
	Recipient   string // required
	Kind        Kind   // required
	Title       string // required
	SubjectPath string // optional — page or data key the item is about
	SubjectRef  string // optional — anchor (card id, etc.)
	Actor       string // who triggered the event; empty for system
}

// Create inserts a new inbox item. Returns the populated row.
// Deduplication rule (v0): if the same recipient+kind+subject_path+
// subject_ref pair already exists within the last minute AND is not
// yet read, we skip creation. Prevents a burst of identical mentions
// (edit, revert, edit) flooding the inbox.
func (s *Store) Create(p CreateParams) (*Item, error) {
	if strings.TrimSpace(p.Recipient) == "" {
		return nil, errors.New("inbox: recipient required")
	}
	if p.Kind == "" {
		return nil, errors.New("inbox: kind required")
	}
	if strings.TrimSpace(p.Title) == "" {
		return nil, errors.New("inbox: title required")
	}
	now := time.Now().UTC()

	// Dedupe window.
	dupCutoff := now.Add(-1 * time.Minute).Unix()
	row := s.db.QueryRow(`
		SELECT id FROM inbox_items
		WHERE recipient = ? AND kind = ? AND COALESCE(subject_path,'') = ? AND COALESCE(subject_ref,'') = ?
		  AND at >= ? AND read_at IS NULL
		ORDER BY at DESC LIMIT 1`,
		p.Recipient, string(p.Kind), p.SubjectPath, p.SubjectRef, dupCutoff,
	)
	var existing int64
	if err := row.Scan(&existing); err == nil {
		// Return the existing row — caller doesn't need to know this was deduped.
		return s.Get(existing)
	}

	res, err := s.db.Exec(`
		INSERT INTO inbox_items (recipient, kind, subject_path, subject_ref, title, actor, at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.Recipient, string(p.Kind),
		nullIfEmpty(p.SubjectPath), nullIfEmpty(p.SubjectRef),
		p.Title, nullIfEmpty(p.Actor), now.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("inbox: insert: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.Get(id)
}

// Get fetches a single item by ID.
func (s *Store) Get(id int64) (*Item, error) {
	row := s.db.QueryRow(`
		SELECT id, recipient, kind, subject_path, subject_ref, title, actor, at, read_at, archived_at
		FROM inbox_items WHERE id = ?`, id)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return it, err
}

// ListParams filters the inbox for a recipient.
type ListParams struct {
	Recipient  string
	UnreadOnly bool
	Limit      int // 0 = default 50
}

// List returns recent items for the recipient, newest first. Archived
// items are excluded (operators can still hard-delete them later).
func (s *Store) List(p ListParams) ([]*Item, error) {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	var rows *sql.Rows
	var err error
	if p.UnreadOnly {
		rows, err = s.db.Query(`
			SELECT id, recipient, kind, subject_path, subject_ref, title, actor, at, read_at, archived_at
			FROM inbox_items
			WHERE recipient = ? AND read_at IS NULL AND archived_at IS NULL
			ORDER BY at DESC, id DESC LIMIT ?`, p.Recipient, p.Limit)
	} else {
		rows, err = s.db.Query(`
			SELECT id, recipient, kind, subject_path, subject_ref, title, actor, at, read_at, archived_at
			FROM inbox_items
			WHERE recipient = ? AND archived_at IS NULL
			ORDER BY at DESC, id DESC LIMIT ?`, p.Recipient, p.Limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// UnreadCount returns the number of unread, non-archived items for a
// recipient. Cheap — indexed.
func (s *Store) UnreadCount(recipient string) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM inbox_items
		WHERE recipient = ? AND read_at IS NULL AND archived_at IS NULL`,
		recipient).Scan(&n)
	return n, err
}

// MarkRead stamps an item as read. Idempotent — calling twice keeps
// the first read_at. Errors with ErrForbidden if the item belongs to
// someone else.
func (s *Store) MarkRead(id int64, recipient string) error {
	it, err := s.Get(id)
	if err != nil {
		return err
	}
	if it.Recipient != recipient {
		return ErrForbidden
	}
	_, err = s.db.Exec(`
		UPDATE inbox_items SET read_at = COALESCE(read_at, ?) WHERE id = ?`,
		time.Now().UTC().Unix(), id)
	return err
}

// MarkAllRead stamps every unread item for a recipient. Useful for
// "inbox zero" buttons.
func (s *Store) MarkAllRead(recipient string) (int, error) {
	res, err := s.db.Exec(`
		UPDATE inbox_items SET read_at = ?
		WHERE recipient = ? AND read_at IS NULL AND archived_at IS NULL`,
		time.Now().UTC().Unix(), recipient)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Archive hides an item from the default list without deleting.
func (s *Store) Archive(id int64, recipient string) error {
	it, err := s.Get(id)
	if err != nil {
		return err
	}
	if it.Recipient != recipient {
		return ErrForbidden
	}
	_, err = s.db.Exec(`
		UPDATE inbox_items SET archived_at = COALESCE(archived_at, ?) WHERE id = ?`,
		time.Now().UTC().Unix(), id)
	return err
}

// Delete removes an item permanently. Recipient-gated.
func (s *Store) Delete(id int64, recipient string) error {
	it, err := s.Get(id)
	if err != nil {
		return err
	}
	if it.Recipient != recipient {
		return ErrForbidden
	}
	_, err = s.db.Exec(`DELETE FROM inbox_items WHERE id = ?`, id)
	return err
}

// PurgeOlderThan hard-deletes items older than the cutoff. Called
// periodically to keep the table bounded. Retention default is 60
// days — see the cron stub in handlers.
func (s *Store) PurgeOlderThan(cutoff time.Time) (int, error) {
	res, err := s.db.Exec(`DELETE FROM inbox_items WHERE at < ?`, cutoff.UTC().Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// -------- scanners --------

type scanner interface {
	Scan(dest ...any) error
}

func scanItem(sc scanner) (*Item, error) {
	var (
		it          Item
		subjectPath sql.NullString
		subjectRef  sql.NullString
		actor       sql.NullString
		at          int64
		readAt      sql.NullInt64
		archivedAt  sql.NullInt64
		kind        string
	)
	err := sc.Scan(
		&it.ID, &it.Recipient, &kind, &subjectPath, &subjectRef,
		&it.Title, &actor, &at, &readAt, &archivedAt,
	)
	if err != nil {
		return nil, err
	}
	it.Kind = Kind(kind)
	if subjectPath.Valid {
		it.SubjectPath = subjectPath.String
	}
	if subjectRef.Valid {
		it.SubjectRef = subjectRef.String
	}
	if actor.Valid {
		it.Actor = actor.String
	}
	it.At = time.Unix(at, 0).UTC()
	if readAt.Valid {
		t := time.Unix(readAt.Int64, 0).UTC()
		it.ReadAt = &t
	}
	if archivedAt.Valid {
		t := time.Unix(archivedAt.Int64, 0).UTC()
		it.ArchivedAt = &t
	}
	return &it, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
