package invitations

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "inv.sqlite")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateAndGet(t *testing.T) {
	s, err := NewStore(openDB(t))
	if err != nil {
		t.Fatal(err)
	}
	inv, err := s.Create(CreateParams{
		Role:      RoleMember,
		CreatedBy: "alice",
		ExpiresIn: 7 * 24 * time.Hour,
		Label:     "design team",
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.ID[:4] != "inv_" {
		t.Errorf("id prefix = %q", inv.ID[:4])
	}
	if inv.Status() != "active" {
		t.Errorf("status = %q", inv.Status())
	}

	got, err := s.Get(inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != RoleMember || got.Label != "design team" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestCreateInvalidRole(t *testing.T) {
	s, _ := NewStore(openDB(t))
	if _, err := s.Create(CreateParams{Role: "agent", CreatedBy: "alice"}); err != ErrInvalidRole {
		t.Errorf("expected ErrInvalidRole, got %v", err)
	}
}

func TestRedeemHappyPath(t *testing.T) {
	s, _ := NewStore(openDB(t))
	inv, _ := s.Create(CreateParams{Role: RoleAdmin, CreatedBy: "alice"})
	redeemed, err := s.Redeem(inv.ID, "dana")
	if err != nil {
		t.Fatal(err)
	}
	if redeemed.RedeemedBy != "dana" {
		t.Errorf("redeemed_by = %q", redeemed.RedeemedBy)
	}
	if redeemed.Status() != "redeemed" {
		t.Errorf("status after redeem = %q", redeemed.Status())
	}
	// Second redeem fails.
	if _, err := s.Redeem(inv.ID, "eve"); err != ErrAlreadyRedeemed {
		t.Errorf("expected ErrAlreadyRedeemed, got %v", err)
	}
}

func TestRedeemExpired(t *testing.T) {
	s, _ := NewStore(openDB(t))
	inv, _ := s.Create(CreateParams{Role: RoleMember, CreatedBy: "alice", ExpiresIn: 1 * time.Millisecond})
	time.Sleep(20 * time.Millisecond)
	if _, err := s.Redeem(inv.ID, "dana"); err != ErrExpired {
		t.Errorf("expected ErrExpired, got %v", err)
	}
}

func TestRedeemRevoked(t *testing.T) {
	s, _ := NewStore(openDB(t))
	inv, _ := s.Create(CreateParams{Role: RoleMember, CreatedBy: "alice"})
	if err := s.Revoke(inv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redeem(inv.ID, "dana"); err != ErrRevoked {
		t.Errorf("expected ErrRevoked, got %v", err)
	}
	if err := s.Revoke(inv.ID); err != nil {
		t.Errorf("re-revoke should be idempotent: %v", err)
	}
}

func TestRedeemNotFound(t *testing.T) {
	s, _ := NewStore(openDB(t))
	if _, err := s.Redeem("inv_nope", "dana"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestConcurrentRedeem(t *testing.T) {
	s, _ := NewStore(openDB(t))
	inv, _ := s.Create(CreateParams{Role: RoleMember, CreatedBy: "alice"})

	// Retry SQLITE_BUSY inside the worker — production callers wrap
	// the redeem in the same tx they use to create the user + token,
	// and the outer transaction retry absorbs the busy signal. We
	// simulate that here.
	redeem := func() error {
		for i := 0; i < 20; i++ {
			_, err := s.Redeem(inv.ID, "user")
			if err == nil {
				return nil
			}
			if err == ErrAlreadyRedeemed || err == ErrExpired || err == ErrRevoked {
				return err
			}
			time.Sleep(5 * time.Millisecond)
		}
		return nil
	}

	var wg sync.WaitGroup
	results := make([]error, 5)
	for i := range results {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = redeem()
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		} else if err != ErrAlreadyRedeemed {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 success, got %d", successes)
	}
}

func TestBootstrapActive(t *testing.T) {
	s, _ := NewStore(openDB(t))
	// Initially no bootstrap invite.
	active, err := s.BootstrapActive()
	if err != nil || active != nil {
		t.Errorf("initial BootstrapActive: %v %v", active, err)
	}
	// Mint one.
	inv, _ := s.Create(CreateParams{Role: RoleAdmin, CreatedBy: BootstrapCreator, ExpiresIn: 24 * time.Hour})
	active, _ = s.BootstrapActive()
	if active == nil || active.ID != inv.ID {
		t.Errorf("BootstrapActive = %v, want %q", active, inv.ID)
	}
	// Attempting to mint a SECOND bootstrap invite hits the partial unique index.
	if _, err := s.Create(CreateParams{Role: RoleAdmin, CreatedBy: BootstrapCreator, ExpiresIn: 24 * time.Hour}); err == nil {
		t.Error("expected second bootstrap mint to collide on unique index")
	}
	// Redeem, then next BootstrapActive returns nil so a fresh one can be minted.
	_, _ = s.Redeem(inv.ID, "alice")
	active, _ = s.BootstrapActive()
	if active != nil {
		t.Errorf("after redeem, BootstrapActive = %v, want nil", active)
	}
	// Mint a fresh bootstrap invite is now allowed.
	if _, err := s.Create(CreateParams{Role: RoleAdmin, CreatedBy: BootstrapCreator, ExpiresIn: 24 * time.Hour}); err != nil {
		t.Errorf("post-redeem bootstrap mint: %v", err)
	}
}

func TestList(t *testing.T) {
	s, _ := NewStore(openDB(t))
	active, _ := s.Create(CreateParams{Role: RoleMember, CreatedBy: "alice"})
	expired, _ := s.Create(CreateParams{Role: RoleMember, CreatedBy: "alice", ExpiresIn: 1 * time.Millisecond})
	time.Sleep(20 * time.Millisecond)
	revoked, _ := s.Create(CreateParams{Role: RoleMember, CreatedBy: "alice"})
	_ = s.Revoke(revoked.ID)

	all, _ := s.List(true)
	if len(all) != 3 {
		t.Errorf("List(true) = %d, want 3", len(all))
	}
	activeOnly, _ := s.List(false)
	if len(activeOnly) != 1 || activeOnly[0].ID != active.ID {
		t.Errorf("List(false) = %v, want [%s]", activeOnly, active.ID)
	}
	// Sanity: expired detected by Status.
	got, _ := s.Get(expired.ID)
	if got.Status() != "expired" {
		t.Errorf("expired status = %q", got.Status())
	}
}
