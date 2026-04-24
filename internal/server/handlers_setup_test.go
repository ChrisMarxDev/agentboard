package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// bareClient bypasses the global test auth transport so we can hit open
// endpoints without a pre-seeded bearer.
func bareClient() *http.Client { return &http.Client{} }

func TestSetup_StatusAndClaim(t *testing.T) {
	// Build an unauthenticated test server — no seeded user, no transport.
	ts := newAuthedTestServer(t, "")

	// Status is open and reports not initialized.
	r, err := bareClient().Get(ts.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("status = %d", r.StatusCode)
	}
	var status struct{ Initialized bool }
	json.NewDecoder(r.Body).Decode(&status)
	if status.Initialized {
		t.Error("freshly-made board reports initialized=true")
	}

	// Claim — POST /api/setup with a username.
	body := strings.NewReader(`{"username":"alice","display_name":"Alice Chen"}`)
	resp, err := bareClient().Post(ts.URL+"/api/setup", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("claim = %d", resp.StatusCode)
	}
	var claim struct {
		Username string `json:"username"`
		Token    string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&claim)
	if claim.Username != "alice" {
		t.Errorf("claim.username = %q", claim.Username)
	}
	if !strings.HasPrefix(claim.Token, "ab_") {
		t.Errorf("token shape: %s", claim.Token)
	}

	// Status now reports initialized.
	r2, _ := bareClient().Get(ts.URL + "/api/setup/status")
	var s2 struct{ Initialized bool }
	json.NewDecoder(r2.Body).Decode(&s2)
	r2.Body.Close()
	if !s2.Initialized {
		t.Error("post-claim initialized=false")
	}

	// Second claim must 409.
	body2 := strings.NewReader(`{"username":"bob"}`)
	resp2, err := bareClient().Post(ts.URL+"/api/setup", "application/json", body2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 409 {
		t.Errorf("second claim = %d, want 409", resp2.StatusCode)
	}

	// The returned token works as admin.
	req, _ := http.NewRequest("GET", ts.URL+"/api/admin/me", nil)
	req.Header.Set("Authorization", "Bearer "+claim.Token)
	meResp, err := bareClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer meResp.Body.Close()
	if meResp.StatusCode != 200 {
		t.Errorf("claim token /api/admin/me = %d", meResp.StatusCode)
	}
	var me struct {
		Username string `json:"username"`
		Kind     string `json:"kind"`
	}
	json.NewDecoder(meResp.Body).Decode(&me)
	if me.Username != "alice" || me.Kind != "admin" {
		t.Errorf("me = %+v, want alice/admin", me)
	}
}

func TestSetup_InvalidUsername(t *testing.T) {
	ts := newAuthedTestServer(t, "")
	body := strings.NewReader(`{"username":"0bad"}`)
	resp, err := bareClient().Post(ts.URL+"/api/setup", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("invalid username = %d, want 400", resp.StatusCode)
	}
}

func TestSetup_LockedAfterLegacyMigration(t *testing.T) {
	// When AGENTBOARD_AUTH_TOKEN is set on first boot it produces a
	// legacy-agent user — which counts as "initialized" even though no
	// admin exists. Setup must refuse so the operator uses the CLI to
	// mint an admin (rather than a stranger claiming via /setup).
	ts := newAuthedTestServer(t, "legacy-secret")

	r, err := bareClient().Get(ts.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var s struct{ Initialized bool }
	json.NewDecoder(r.Body).Decode(&s)
	if !s.Initialized {
		t.Error("legacy-migrated board reports initialized=false")
	}

	body := strings.NewReader(`{"username":"alice"}`)
	resp, err := bareClient().Post(ts.URL+"/api/setup", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Errorf("claim after legacy migration = %d, want 409", resp.StatusCode)
	}
}
