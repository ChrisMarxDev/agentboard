package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/christophermarx/agentboard/internal/auth"
)

// seedAdmin creates one admin user + token and returns the plaintext token.
func seedAdmin(t *testing.T, srv *Server) string {
	t.Helper()
	if _, err := srv.Auth.CreateUser(auth.CreateUserParams{
		Username: "admin",
		Kind:     auth.KindAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	tok, _ := auth.GenerateToken()
	if _, err := srv.Auth.CreateToken(auth.CreateTokenParams{
		Username:  "admin",
		TokenHash: auth.HashToken(tok),
		Label:     "test",
	}); err != nil {
		t.Fatal(err)
	}
	return tok
}

func seedAgent(t *testing.T, srv *Server, username string) string {
	t.Helper()
	if _, err := srv.Auth.CreateUser(auth.CreateUserParams{
		Username: username,
		Kind:     auth.KindMember,
	}); err != nil {
		t.Fatal(err)
	}
	tok, _ := auth.GenerateToken()
	if _, err := srv.Auth.CreateToken(auth.CreateTokenParams{
		Username:  username,
		TokenHash: auth.HashToken(tok),
	}); err != nil {
		t.Fatal(err)
	}
	return tok
}

func authReq(t *testing.T, method, url, body, token string) *http.Request {
	t.Helper()
	var req *http.Request
	var err error
	if body == "" {
		req, err = http.NewRequest(method, url, nil)
	} else {
		req, err = http.NewRequest(method, url, strings.NewReader(body))
	}
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestAdmin_RequiresAdminToken(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	agentToken := seedAgent(t, srv, "bot")

	// newTestServer installs a global DefaultClient transport that
	// auto-injects the test-agent token. For the "no token" check we
	// need a fresh client that bypasses that injection.
	bareClient := &http.Client{}

	noTok, _ := http.NewRequest("GET", ts.URL+"/api/admin/me", nil)
	r1, _ := bareClient.Do(noTok)
	if r1.StatusCode != 401 {
		t.Errorf("no token = %d, want 401", r1.StatusCode)
	}
	r1.Body.Close()

	r2, _ := http.DefaultClient.Do(authReq(t, "GET", ts.URL+"/api/admin/me", "", agentToken))
	if r2.StatusCode != 403 {
		t.Errorf("agent on admin = %d, want 403", r2.StatusCode)
	}
	r2.Body.Close()

	r3, _ := http.DefaultClient.Do(authReq(t, "GET", ts.URL+"/api/admin/me", "", adminToken))
	if r3.StatusCode != 200 {
		t.Errorf("admin on admin = %d, want 200", r3.StatusCode)
	}
	r3.Body.Close()
}

func TestAdmin_CreateUser_ReturnsTokenOnce(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)

	// Restrict to GETs under /api/v2/data/** — the data surface during
	// the rewrite. Cut 3 collapses this back to /api/data/**.
	body := `{"username":"viewer","kind":"member","access_mode":"restrict_to_list","rules":[{"action":"allow","pattern":"/api/v2/data/**","methods":["GET"]}]}`
	resp, err := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/admin/users", body, adminToken))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		t.Fatal(err)
	}
	if tok.Username != "viewer" {
		t.Errorf("unexpected username: %s", tok.Username)
	}
	if !strings.HasPrefix(tok.Token, "ab_") {
		t.Errorf("bad token shape: %s", tok.Token)
	}

	// Viewer GET passes auth (rule allows /api/v2/data/** GETs); the
	// key may not exist yet, so 200 OR 404 both mean "auth let me
	// through." 401/403 would mean the rule didn't apply.
	gr, _ := http.NewRequest("GET", ts.URL+"/api/v2/data/foo", nil)
	gr.Header.Set("Authorization", "Bearer "+tok.Token)
	gresp, _ := http.DefaultClient.Do(gr)
	if gresp.StatusCode != 200 && gresp.StatusCode != 404 {
		t.Errorf("GET = %d, want 200 or 404 (auth pass)", gresp.StatusCode)
	}
	gresp.Body.Close()

	pr, _ := http.NewRequest("PUT", ts.URL+"/api/v2/data/foo", strings.NewReader(`{"value":"x"}`))
	pr.Header.Set("Content-Type", "application/json")
	pr.Header.Set("Authorization", "Bearer "+tok.Token)
	presp, _ := http.DefaultClient.Do(pr)
	if presp.StatusCode != 403 {
		t.Errorf("PUT = %d, want 403", presp.StatusCode)
	}
	presp.Body.Close()
}

func TestAdmin_UsernameTaken_AndReservedAfterDeactivate(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)

	// Create and deactivate a user.
	resp, _ := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/admin/users",
		`{"username":"alice","kind":"member"}`, adminToken))
	resp.Body.Close()
	deact, _ := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/admin/users/alice/deactivate", "", adminToken))
	deact.Body.Close()

	// Re-creating "alice" must 409 even though she's deactivated.
	again, err := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/admin/users",
		`{"username":"alice","kind":"member"}`, adminToken))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Body.Close()
	if again.StatusCode != 409 {
		t.Errorf("re-create deactivated username = %d, want 409", again.StatusCode)
	}
}

func TestAdmin_InvalidUsername(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)

	resp, err := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/admin/users",
		`{"username":"0bad","kind":"member"}`, adminToken))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAdmin_UpdateUser_CannotChangeUsername(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	seedAgent(t, srv, "alice")

	// The PATCH body doesn't even accept username — it's not in the request
	// struct. Sending one should be ignored; the user keeps their original
	// username. Verify by sending a username field and checking the response
	// still says alice.
	resp, err := http.DefaultClient.Do(authReq(t, "PATCH", ts.URL+"/api/admin/users/alice",
		`{"username":"notAlice","display_name":"Alice Chen"}`, adminToken))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var user struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}
	json.NewDecoder(resp.Body).Decode(&user)
	if user.Username != "alice" {
		t.Errorf("username mutated via PATCH: got %q", user.Username)
	}
	if user.DisplayName != "Alice Chen" {
		t.Errorf("display_name not updated: got %q", user.DisplayName)
	}
}

func TestAdmin_RotateToken(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)

	resp, _ := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/admin/users",
		`{"username":"rotator","kind":"member"}`, adminToken))
	var created tokenResponse
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	oldToken := created.Token

	resp2, _ := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/"+created.Username+"/tokens/"+created.TokenID+"/rotate",
		"", adminToken))
	var rotated tokenResponse
	json.NewDecoder(resp2.Body).Decode(&rotated)
	resp2.Body.Close()
	if rotated.Token == oldToken {
		t.Error("rotated token should differ")
	}

	gr, _ := http.NewRequest("GET", ts.URL+"/api/me", nil)
	gr.Header.Set("Authorization", "Bearer "+oldToken)
	r, _ := http.DefaultClient.Do(gr)
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Errorf("old token = %d, want 401", r.StatusCode)
	}
	gr2, _ := http.NewRequest("GET", ts.URL+"/api/me", nil)
	gr2.Header.Set("Authorization", "Bearer "+rotated.Token)
	r2, _ := http.DefaultClient.Do(gr2)
	r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Errorf("new token = %d, want 200", r2.StatusCode)
	}
}

func TestAdmin_CannotDeactivateSelf(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)

	r, _ := http.DefaultClient.Do(authReq(t, "GET", ts.URL+"/api/admin/me", "", adminToken))
	var me meResponse
	json.NewDecoder(r.Body).Decode(&me)
	r.Body.Close()

	r2, err := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/admin/users/"+me.Username+"/deactivate", "", adminToken))
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != 400 {
		t.Errorf("self-deactivate = %d, want 400", r2.StatusCode)
	}
}

func TestAdmin_MultipleTokens(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)

	r, _ := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/admin/users",
		`{"username":"multi","kind":"member"}`, adminToken))
	var first tokenResponse
	json.NewDecoder(r.Body).Decode(&first)
	r.Body.Close()

	r2, _ := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/"+first.Username+"/tokens",
		`{"label":"ci"}`, adminToken))
	var second tokenResponse
	json.NewDecoder(r2.Body).Decode(&second)
	r2.Body.Close()
	if first.Token == second.Token {
		t.Error("second token should differ")
	}

	for _, tok := range []string{first.Token, second.Token} {
		gr, _ := http.NewRequest("GET", ts.URL+"/api/me", nil)
		gr.Header.Set("Authorization", "Bearer "+tok)
		g, _ := http.DefaultClient.Do(gr)
		g.Body.Close()
		if g.StatusCode != 200 {
			t.Errorf("token returned %d", g.StatusCode)
		}
	}

	rv, _ := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/"+first.Username+"/tokens/"+first.TokenID+"/revoke",
		"", adminToken))
	rv.Body.Close()

	gr1, _ := http.NewRequest("GET", ts.URL+"/api/me", nil)
	gr1.Header.Set("Authorization", "Bearer "+first.Token)
	g1, _ := http.DefaultClient.Do(gr1)
	g1.Body.Close()
	if g1.StatusCode != 401 {
		t.Errorf("revoked = %d, want 401", g1.StatusCode)
	}

	gr2, _ := http.NewRequest("GET", ts.URL+"/api/me", nil)
	gr2.Header.Set("Authorization", "Bearer "+second.Token)
	g2, _ := http.DefaultClient.Do(gr2)
	g2.Body.Close()
	if g2.StatusCode != 200 {
		t.Errorf("second token = %d, want 200", g2.StatusCode)
	}
}

func TestUsersDirectory_AgentReadable(t *testing.T) {
	srv, ts := newTestServer(t)
	_ = seedAdmin(t, srv)
	agentToken := seedAgent(t, srv, "agent1")
	_ = seedAgent(t, srv, "agent2")

	r, err := http.DefaultClient.Do(authReq(t, "GET", ts.URL+"/api/users", "", agentToken))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("list users = %d", r.StatusCode)
	}
	var body struct {
		Users []struct {
			Username string `json:"username"`
		} `json:"users"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if len(body.Users) < 3 {
		t.Errorf("want >=3 users, got %d", len(body.Users))
	}
}

func TestUsersResolve(t *testing.T) {
	srv, ts := newTestServer(t)
	_ = seedAdmin(t, srv)
	agentToken := seedAgent(t, srv, "alice")
	_ = seedAgent(t, srv, "bob")

	r, err := http.DefaultClient.Do(authReq(t, "POST", ts.URL+"/api/users/resolve",
		`{"usernames":["alice","nobody","BOB"]}`, agentToken))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("resolve = %d", r.StatusCode)
	}
	var body struct {
		Users []struct{ Username string } `json:"users"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if len(body.Users) != 2 {
		t.Errorf("want 2 resolved, got %d", len(body.Users))
	}
}
