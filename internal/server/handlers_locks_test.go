package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/christophermarx/agentboard/internal/auth"
)

// Setup: seed an admin + a non-admin member, seed a page, verify lock
// enforcement on write / delete / move / bulk-delete. Also verifies the
// X-Locked-* headers on GET.

func seedPage(t *testing.T, srv *Server, path, content string) {
	t.Helper()
	if err := srv.Pages.WritePage(path, content); err != nil {
		t.Fatalf("seed page %s: %v", path, err)
	}
}

func TestLock_AdminCanLock(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	seedPage(t, srv, "handbook", "# Handbook\n\nContent.")

	body := `{"path":"handbook","reason":"canonical"}`
	r, err := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/locks", body, adminToken))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 201 {
		t.Errorf("admin lock = %d, want 201", r.StatusCode)
	}
}

func TestLock_MemberCannotLock(t *testing.T) {
	srv, ts := newTestServer(t)
	seedPage(t, srv, "handbook", "# Handbook")
	// default client is a member
	body := `{"path":"handbook"}`
	r, err := http.DefaultClient.Post(ts.URL+"/api/locks", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 403 {
		t.Errorf("member lock = %d, want 403", r.StatusCode)
	}
	_ = srv // silence
}

func TestLock_NonexistentPage(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	body := `{"path":"no-such-page"}`
	r, err := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/locks", body, adminToken))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 404 {
		t.Errorf("nonexistent page lock = %d, want 404", r.StatusCode)
	}
}

func TestLock_WriteGate(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	seedPage(t, srv, "handbook", "# Handbook")
	// Lock it.
	r, _ := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/locks",
		`{"path":"handbook","reason":"canonical"}`, adminToken))
	r.Body.Close()

	// Member write → 403 PAGE_LOCKED.
	req, _ := http.NewRequest("PUT", ts.URL+"/api/handbook", strings.NewReader("# Handbook edited"))
	req.Header.Set("Content-Type", "text/markdown")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != 403 {
		t.Errorf("member write on locked page = %d, want 403", r2.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(r2.Body).Decode(&body)
	if body["code"] != "PAGE_LOCKED" {
		t.Errorf("code = %v, want PAGE_LOCKED", body["code"])
	}

	// Admin write → 200.
	req3, _ := http.NewRequest("PUT", ts.URL+"/api/handbook", strings.NewReader("# Handbook v2"))
	req3.Header.Set("Content-Type", "text/markdown")
	req3.Header.Set("Authorization", "Bearer "+adminToken)
	r3, _ := http.DefaultClient.Do(req3)
	if r3.StatusCode != 200 {
		t.Errorf("admin write on locked page = %d, want 200", r3.StatusCode)
	}
	r3.Body.Close()
}

func TestLock_DeleteGate(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	seedPage(t, srv, "handbook", "# Handbook")
	r, _ := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/locks",
		`{"path":"handbook"}`, adminToken))
	r.Body.Close()

	// Member delete → 403.
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/handbook", nil)
	r2, _ := http.DefaultClient.Do(req)
	if r2.StatusCode != 403 {
		t.Errorf("member delete on locked page = %d, want 403", r2.StatusCode)
	}
	r2.Body.Close()
}

func TestLock_MoveTravels(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	seedPage(t, srv, "handbook", "# Handbook")
	r, _ := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/locks",
		`{"path":"handbook","reason":"v1"}`, adminToken))
	r.Body.Close()

	// Admin moves the page.
	mv, _ := json.Marshal(map[string]string{"from": "handbook", "to": "docs/handbook"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/content/move", bytes.NewReader(mv))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Fatalf("move = %d", r2.StatusCode)
	}

	// Lock row should now be at the new path.
	lock, err := srv.Locks.Get("docs/handbook")
	if err != nil || lock == nil {
		t.Errorf("lock didn't travel to docs/handbook: %v", err)
	}
	if srv.Locks.IsLocked("handbook") {
		t.Error("old path still locked")
	}
	if lock.Reason != "v1" {
		t.Errorf("lock reason = %q", lock.Reason)
	}
}

func TestLock_GetPageEmitsHeaders(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	seedPage(t, srv, "handbook", "# Handbook")
	r, _ := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/locks",
		`{"path":"handbook","reason":"canonical onboarding"}`, adminToken))
	r.Body.Close()

	r2, err := http.DefaultClient.Get(ts.URL + "/api/handbook")
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.Header.Get("X-Locked-By") == "" {
		t.Error("X-Locked-By header missing")
	}
	if r2.Header.Get("X-Locked-Reason") != "canonical onboarding" {
		t.Errorf("X-Locked-Reason = %q", r2.Header.Get("X-Locked-Reason"))
	}
}

func TestLock_Unlock(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	seedPage(t, srv, "handbook", "# Handbook")
	r, _ := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/locks",
		`{"path":"handbook"}`, adminToken))
	r.Body.Close()

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/locks/handbook", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Errorf("unlock = %d", r2.StatusCode)
	}
	if srv.Locks.IsLocked("handbook") {
		t.Error("still locked after unlock")
	}
	// Member can now write.
	req3, _ := http.NewRequest("PUT", ts.URL+"/api/handbook", strings.NewReader("# After unlock"))
	req3.Header.Set("Content-Type", "text/markdown")
	r3, _ := http.DefaultClient.Do(req3)
	if r3.StatusCode != 200 {
		t.Errorf("member write post-unlock = %d", r3.StatusCode)
	}
	r3.Body.Close()
}

// Unused var _ to silence auth import when compiling without reference
var _ = auth.KindAdmin
