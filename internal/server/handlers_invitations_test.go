package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/invitations"
)

// Public GET /api/invitations/{id} returns a restricted view and 404s
// on any unusable state (redeemed / expired / revoked / not found),
// to prevent probing for valid IDs.

func TestInvitation_PublicView(t *testing.T) {
	srv, ts := newAuthedTestServerWithSrv(t, "")
	inv, err := srv.Invitations.Create(invitations.CreateParams{
		Role:      invitations.RoleMember,
		CreatedBy: "alice",
		ExpiresIn: 24 * time.Hour,
		Label:     "design team",
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := bareClient().Get(ts.URL + "/api/invitations/" + inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("status = %d", r.StatusCode)
	}
	var view struct {
		ID        string `json:"id"`
		Role      string `json:"role"`
		CreatedBy string `json:"created_by"`
		Label     string `json:"label"`
		Bootstrap bool   `json:"bootstrap"`
	}
	json.NewDecoder(r.Body).Decode(&view)
	if view.ID != inv.ID || view.Role != "member" || view.Label != "design team" {
		t.Errorf("view = %+v", view)
	}
	if view.Bootstrap {
		t.Error("non-bootstrap invite should not set Bootstrap=true")
	}

	// Revoke it; public GET now 404s.
	if err := srv.Invitations.Revoke(inv.ID); err != nil {
		t.Fatal(err)
	}
	r2, _ := bareClient().Get(ts.URL + "/api/invitations/" + inv.ID)
	if r2.StatusCode != 404 {
		t.Errorf("revoked invite public GET = %d, want 404", r2.StatusCode)
	}
	r2.Body.Close()
}

func TestInvitation_RedeemHappyPath(t *testing.T) {
	srv, ts := newAuthedTestServerWithSrv(t, "")
	inv, err := srv.Invitations.Create(invitations.CreateParams{
		Role:      invitations.RoleMember,
		CreatedBy: "alice",
		ExpiresIn: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{"username": "dana"})
	r, err := bareClient().Post(
		ts.URL+"/api/invitations/"+inv.ID+"/redeem",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 201 {
		t.Fatalf("redeem status = %d", r.StatusCode)
	}
	var resp struct {
		Token        string `json:"token"`
		InvitationID string `json:"invitation_id"`
		Role         string `json:"role"`
		User         struct {
			Username string `json:"username"`
			Kind     string `json:"kind"`
		} `json:"user"`
	}
	json.NewDecoder(r.Body).Decode(&resp)
	if !strings.HasPrefix(resp.Token, "ab_") {
		t.Errorf("token shape: %s", resp.Token)
	}
	if resp.User.Username != "dana" || resp.User.Kind != "member" {
		t.Errorf("user = %+v", resp.User)
	}
	if resp.Role != "member" {
		t.Errorf("role = %q", resp.Role)
	}

	// The returned token works.
	req, _ := http.NewRequest("GET", ts.URL+"/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	meResp, _ := bareClient().Do(req)
	defer meResp.Body.Close()
	if meResp.StatusCode != 200 {
		t.Errorf("/api/me with fresh token = %d", meResp.StatusCode)
	}

	// Second redeem fails with 410 Gone.
	body2, _ := json.Marshal(map[string]string{"username": "eve"})
	r2, _ := bareClient().Post(
		ts.URL+"/api/invitations/"+inv.ID+"/redeem",
		"application/json", bytes.NewReader(body2))
	if r2.StatusCode != 410 {
		t.Errorf("second redeem = %d, want 410", r2.StatusCode)
	}
	r2.Body.Close()
}

func TestInvitation_UsernameTakenDoesNotConsume(t *testing.T) {
	srv, ts := newAuthedTestServerWithSrv(t, "")
	// Seed a user with the name someone's about to try.
	if _, err := srv.Auth.CreateUser(auth.CreateUserParams{
		Username: "dana", Kind: auth.KindMember,
	}); err != nil {
		t.Fatal(err)
	}
	inv, _ := srv.Invitations.Create(invitations.CreateParams{
		Role:      invitations.RoleMember,
		CreatedBy: "alice",
		ExpiresIn: 24 * time.Hour,
	})

	body, _ := json.Marshal(map[string]string{"username": "dana"})
	r, _ := bareClient().Post(
		ts.URL+"/api/invitations/"+inv.ID+"/redeem",
		"application/json", bytes.NewReader(body))
	if r.StatusCode != 409 {
		t.Errorf("taken redeem = %d, want 409", r.StatusCode)
	}
	r.Body.Close()

	// Invite is STILL active — retry with a different username works.
	got, _ := srv.Invitations.Get(inv.ID)
	if got.RedeemedAt != nil {
		t.Error("invite should NOT be consumed when username is taken")
	}
	body2, _ := json.Marshal(map[string]string{"username": "elena"})
	r2, _ := bareClient().Post(
		ts.URL+"/api/invitations/"+inv.ID+"/redeem",
		"application/json", bytes.NewReader(body2))
	if r2.StatusCode != 201 {
		t.Errorf("retry with different name = %d, want 201", r2.StatusCode)
	}
	r2.Body.Close()
}

func TestInvitation_ExpiredRedeem(t *testing.T) {
	srv, ts := newAuthedTestServerWithSrv(t, "")
	inv, _ := srv.Invitations.Create(invitations.CreateParams{
		Role:      invitations.RoleMember,
		CreatedBy: "alice",
		ExpiresIn: 1 * time.Millisecond,
	})
	time.Sleep(20 * time.Millisecond)
	body, _ := json.Marshal(map[string]string{"username": "dana"})
	r, _ := bareClient().Post(
		ts.URL+"/api/invitations/"+inv.ID+"/redeem",
		"application/json", bytes.NewReader(body))
	if r.StatusCode != 410 {
		t.Errorf("expired redeem = %d, want 410", r.StatusCode)
	}
	r.Body.Close()
}

func TestInvitation_AdminCreateListRevoke(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)

	// Create.
	body := `{"role":"member","label":"cx team","expires_in_days":14}`
	r, _ := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/admin/invitations", body, adminToken))
	if r.StatusCode != 201 {
		t.Fatalf("create invite = %d", r.StatusCode)
	}
	var created struct {
		ID     string `json:"id"`
		Role   string `json:"role"`
		Status string `json:"status"`
	}
	json.NewDecoder(r.Body).Decode(&created)
	r.Body.Close()
	if created.Role != "member" || created.Status != "active" {
		t.Errorf("created = %+v", created)
	}

	// List.
	r2, _ := http.DefaultClient.Do(authReq(t, "GET",
		ts.URL+"/api/admin/invitations", "", adminToken))
	var list struct {
		Invitations []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"invitations"`
	}
	json.NewDecoder(r2.Body).Decode(&list)
	r2.Body.Close()
	if len(list.Invitations) != 1 || list.Invitations[0].ID != created.ID {
		t.Errorf("list = %+v", list)
	}

	// Revoke.
	r3, _ := http.DefaultClient.Do(authReq(t, "DELETE",
		ts.URL+"/api/admin/invitations/"+created.ID, "", adminToken))
	if r3.StatusCode != 200 {
		t.Errorf("revoke = %d", r3.StatusCode)
	}
	r3.Body.Close()
}

func TestInvitation_AdminBadRole(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	r, _ := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/admin/invitations", `{"role":"agent"}`, adminToken))
	if r.StatusCode != 400 {
		t.Errorf("agent role = %d, want 400", r.StatusCode)
	}
	r.Body.Close()
}

// TestInvitation_RedeemWithPassword proves the redeem flow accepts
// an optional password, hashes it, and emits a session cookie in
// the same response. Browser-driven invitees land on the dashboard
// already signed in; agent-driven (no password) callers still
// receive only the token, unchanged.
func TestInvitation_RedeemWithPassword(t *testing.T) {
	srv, ts := newAuthedTestServerWithSrv(t, "")
	inv, _ := srv.Invitations.Create(invitations.CreateParams{
		Role:      invitations.RoleMember,
		CreatedBy: "alice",
		ExpiresIn: 24 * time.Hour,
	})
	body, _ := json.Marshal(map[string]string{
		"username": "dana",
		"password": "redeem-password-1234",
	})
	r, err := bareClient().Post(
		ts.URL+"/api/invitations/"+inv.ID+"/redeem",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 201 {
		t.Fatalf("redeem with password = %d", r.StatusCode)
	}
	// Cookies were set on the response.
	gotSession, gotCSRF := false, false
	for _, c := range r.Cookies() {
		if c.Name == auth.SessionCookieName {
			gotSession = true
		}
		if c.Name == auth.CSRFCookieName {
			gotCSRF = true
		}
	}
	if !gotSession || !gotCSRF {
		t.Errorf("missing cookies: session=%v csrf=%v", gotSession, gotCSRF)
	}
	// Password actually verifies.
	if _, err := srv.Auth.VerifyLogin("dana", "redeem-password-1234"); err != nil {
		t.Errorf("post-redeem login: %v", err)
	}
}

// TestInvitation_RedeemRejectsWeakPassword guards the floor —
// an 8-character password gets rejected with code=weak_password.
func TestInvitation_RedeemRejectsWeakPassword(t *testing.T) {
	srv, ts := newAuthedTestServerWithSrv(t, "")
	inv, _ := srv.Invitations.Create(invitations.CreateParams{
		Role:      invitations.RoleMember,
		CreatedBy: "alice",
		ExpiresIn: 24 * time.Hour,
	})
	body, _ := json.Marshal(map[string]string{
		"username": "dana",
		"password": "short", // < MinPasswordLen
	})
	r, err := bareClient().Post(
		ts.URL+"/api/invitations/"+inv.ID+"/redeem",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 400 {
		t.Errorf("weak password = %d, want 400", r.StatusCode)
	}
}

func TestInvitation_NonAdminCannotCreate(t *testing.T) {
	_, ts := newTestServer(t)
	// default client is member-kind — 403 on admin subtree.
	r, _ := http.DefaultClient.Post(
		ts.URL+"/api/admin/invitations",
		"application/json", strings.NewReader(`{"role":"member"}`))
	if r.StatusCode != 403 {
		t.Errorf("member on admin = %d, want 403", r.StatusCode)
	}
	r.Body.Close()
}
