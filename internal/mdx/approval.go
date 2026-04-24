package mdx

import (
	"database/sql"
	"time"
)

// PageApproval is the "a human read this version and said it's correct"
// bit. Approval is per-page and per-etag: when the page is edited the
// stored `approved_etag` stops matching the page's current etag and the
// approval is implicitly stale (the row lives on so the UI can show
// *"approved at vX by @alice, edited since"* rather than silently
// forgetting).
//
// v0.5 scope:
//   - any authenticated user can approve any page (no ACL)
//   - nothing depends on approval for authorisation
//   - revocation is explicit (DELETE); stale approvals are not auto-deleted
type PageApproval struct {
	Path          string `json:"path"`
	ApprovedBy    string `json:"approved_by"`
	ApprovedAt    string `json:"approved_at"`    // RFC3339Nano
	ApprovedEtag  string `json:"approved_etag"`
}

// ApprovalStore persists one PageApproval row per page path.
type ApprovalStore struct {
	db *sql.DB
}

// NewApprovalStore opens (and migrates) the page_approval table.
// Safe to call on every startup.
func NewApprovalStore(db *sql.DB) (*ApprovalStore, error) {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS page_approval (
		path          TEXT PRIMARY KEY,
		approved_by   TEXT NOT NULL,
		approved_at   TEXT NOT NULL,
		approved_etag TEXT NOT NULL
	) STRICT;
	`)
	if err != nil {
		return nil, err
	}
	return &ApprovalStore{db: db}, nil
}

// Approve records an approval for the given path at the current etag.
// Upsert — approving an already-approved page replaces the prior
// approval (new approver, new timestamp, new etag).
func (s *ApprovalStore) Approve(path, actor, etag string) (*PageApproval, error) {
	if actor == "" {
		actor = "anonymous"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`INSERT INTO page_approval (path, approved_by, approved_at, approved_etag)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (path) DO UPDATE SET
		   approved_by = EXCLUDED.approved_by,
		   approved_at = EXCLUDED.approved_at,
		   approved_etag = EXCLUDED.approved_etag`,
		path, actor, now, etag,
	)
	if err != nil {
		return nil, err
	}
	return &PageApproval{Path: path, ApprovedBy: actor, ApprovedAt: now, ApprovedEtag: etag}, nil
}

// Get returns the approval record for a path, or nil if none exists.
// The caller is responsible for comparing `approved_etag` to the
// page's current etag to detect the stale case.
func (s *ApprovalStore) Get(path string) (*PageApproval, error) {
	var a PageApproval
	a.Path = path
	err := s.db.QueryRow(
		`SELECT approved_by, approved_at, approved_etag FROM page_approval WHERE path = ?`, path,
	).Scan(&a.ApprovedBy, &a.ApprovedAt, &a.ApprovedEtag)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Revoke deletes the approval row for a path. Idempotent — revoking
// an unapproved page is a no-op.
func (s *ApprovalStore) Revoke(path string) error {
	_, err := s.db.Exec(`DELETE FROM page_approval WHERE path = ?`, path)
	return err
}

// Delete is an alias for Revoke used when the page itself is being
// deleted, so approval-state doesn't leak across page re-creations.
func (s *ApprovalStore) Delete(path string) error {
	return s.Revoke(path)
}
