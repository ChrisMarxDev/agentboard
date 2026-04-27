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

// /api/setup/status is the only survivor of the v0 setup flow. Auth v1
// replaced POST /api/setup with the invitation-redeem path. These tests
// exercise the status endpoint's new shape — initialized + invite_url.

func TestSetupStatus_Uninitialized_NoBootstrapInvite(t *testing.T) {
	// Fresh board, no bootstrap invite minted yet (newAuthedTestServer
	// doesn't run the serve-path bootstrap).
	ts := newAuthedTestServer(t, "")

	r, err := bareClient().Get(ts.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("status = %d", r.StatusCode)
	}
	var body struct {
		Initialized bool   `json:"initialized"`
		InviteURL   string `json:"invite_url"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Initialized {
		t.Error("freshly-made board reports initialized=true")
	}
	if body.InviteURL != "" {
		t.Errorf("invite_url should be empty with no bootstrap invite; got %q", body.InviteURL)
	}
}

func TestSetupStatus_Uninitialized_WithBootstrapInvite(t *testing.T) {
	srv, ts := newAuthedTestServerWithSrv(t, "")

	// Mint a bootstrap invite directly — the same thing serve.go does.
	inv, err := srv.Auth.BootstrapFirstAdmin(srv.Invitations, "", 0, nil)
	if err != nil || inv == nil {
		t.Fatalf("mint bootstrap: inv=%v err=%v", inv, err)
	}

	r, err := bareClient().Get(ts.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var body struct {
		Initialized bool   `json:"initialized"`
		InviteURL   string `json:"invite_url"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Initialized {
		t.Error("should be unclaimed")
	}
	if !strings.Contains(body.InviteURL, "/invite/"+inv.ID) {
		t.Errorf("invite_url = %q, want to include /invite/%s", body.InviteURL, inv.ID)
	}
}

func TestSetupStatus_Initialized(t *testing.T) {
	// Seed a user via the token param — that flips the board into the
	// initialized state.
	ts := newAuthedTestServer(t, "s3cret")
	r, err := bareClient().Get(ts.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var body struct {
		Initialized bool   `json:"initialized"`
		InviteURL   string `json:"invite_url"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if !body.Initialized {
		t.Error("post-seed board reports initialized=false")
	}
	if body.InviteURL != "" {
		t.Errorf("initialized board should have no invite_url; got %q", body.InviteURL)
	}
}
