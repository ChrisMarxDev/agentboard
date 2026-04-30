package store

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openApprovalDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestApproval_GetEmpty(t *testing.T) {
	s, err := NewApprovalStore(openApprovalDB(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("/handbook")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("Get on unapproved page = %+v, want nil", got)
	}
}

func TestApproval_ApproveThenGet(t *testing.T) {
	s, _ := NewApprovalStore(openApprovalDB(t))
	got, err := s.Approve("/handbook", "alice", "etag-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ApprovedBy != "alice" || got.ApprovedEtag != "etag-1" {
		t.Errorf("Approve returned %+v", got)
	}
	read, err := s.Get("/handbook")
	if err != nil {
		t.Fatal(err)
	}
	if read.ApprovedBy != "alice" || read.ApprovedEtag != "etag-1" {
		t.Errorf("Get returned %+v", read)
	}
	if read.ApprovedAt == "" {
		t.Errorf("approved_at missing")
	}
}

func TestApproval_ApproveReplacesPrior(t *testing.T) {
	s, _ := NewApprovalStore(openApprovalDB(t))
	_, _ = s.Approve("/handbook", "alice", "etag-1")
	_, _ = s.Approve("/handbook", "bob", "etag-2")
	read, _ := s.Get("/handbook")
	if read.ApprovedBy != "bob" || read.ApprovedEtag != "etag-2" {
		t.Errorf("second approval should replace prior: got %+v", read)
	}
}

func TestApproval_Revoke(t *testing.T) {
	s, _ := NewApprovalStore(openApprovalDB(t))
	_, _ = s.Approve("/handbook", "alice", "etag-1")
	if err := s.Revoke("/handbook"); err != nil {
		t.Fatal(err)
	}
	read, _ := s.Get("/handbook")
	if read != nil {
		t.Errorf("Revoke didn't remove approval: %+v", read)
	}
	// Idempotent.
	if err := s.Revoke("/handbook"); err != nil {
		t.Errorf("second Revoke: %v", err)
	}
}

func TestApproval_EmptyActor(t *testing.T) {
	s, _ := NewApprovalStore(openApprovalDB(t))
	got, _ := s.Approve("/x", "", "etag-1")
	if got.ApprovedBy != "anonymous" {
		t.Errorf("empty actor should default to 'anonymous', got %q", got.ApprovedBy)
	}
}
