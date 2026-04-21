package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

// newAdminClient produces an http.Client that keeps cookies, so the session
// cookie set on setup/login is automatically sent on subsequent requests.
func newAdminClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

// bootstrapAdminFlow walks through /setup to create the first admin and
// returns the (authenticated client, csrf token).
func bootstrapAdminFlow(t *testing.T, srv *Server, ts *httptest.Server) (*http.Client, string) {
	t.Helper()

	// Mint a code directly via the store (the CLI / installer would do this).
	code, _, err := srv.Auth.CreateBootstrapCode(60*60*1e9, "test")
	if err != nil {
		t.Fatal(err)
	}

	client := newAdminClient(t)
	body, _ := json.Marshal(setupRequest{Code: code, Name: "alice", Password: "correct-horse"})
	resp, err := client.Post(ts.URL+"/api/admin/setup", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, want 201", resp.StatusCode)
	}
	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	if me.CSRFToken == "" {
		t.Fatal("csrf token should be returned on setup")
	}
	return client, me.CSRFToken
}

func TestAdmin_SetupAndLogin(t *testing.T) {
	srv, ts := newTestServer(t)
	client, _ := bootstrapAdminFlow(t, srv, ts)

	// /me works with the cookie.
	req, _ := http.NewRequest("GET", ts.URL+"/api/admin/me", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("me status = %d, want 200", resp.StatusCode)
	}

	// Login with correct password returns 200.
	login := loginRequest{Name: "alice", Password: "correct-horse"}
	body, _ := json.Marshal(login)
	resp2, err := newAdminClient(t).Post(ts.URL+"/api/admin/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Errorf("login status = %d, want 200", resp2.StatusCode)
	}

	// Wrong password returns 401 and does NOT set a cookie.
	bad := loginRequest{Name: "alice", Password: "wrong"}
	body, _ = json.Marshal(bad)
	resp3, err := newAdminClient(t).Post(ts.URL+"/api/admin/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != 401 {
		t.Errorf("wrong login status = %d, want 401", resp3.StatusCode)
	}
	if len(resp3.Cookies()) > 0 {
		t.Error("no cookie should be set on failed login")
	}
}

func TestAdmin_Me_Requires_Session(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/admin/me")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401 without session", resp.StatusCode)
	}
}

func TestAdmin_CSRF_On_Mutations(t *testing.T) {
	srv, ts := newTestServer(t)
	client, csrf := bootstrapAdminFlow(t, srv, ts)

	body := bytes.NewReader([]byte(`{"name":"bot-1","kind":"agent"}`))
	req, _ := http.NewRequest("POST", ts.URL+"/api/admin/identities", body)
	req.Header.Set("Content-Type", "application/json")
	// Missing X-CSRF-Token.
	resp, _ := client.Do(req)
	if resp.StatusCode != 403 {
		t.Errorf("expected 403 for missing CSRF, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Correct CSRF.
	body2 := bytes.NewReader([]byte(`{"name":"bot-1","kind":"agent"}`))
	req2, _ := http.NewRequest("POST", ts.URL+"/api/admin/identities", body2)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-CSRF-Token", csrf)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 201 {
		t.Errorf("create status = %d, want 201", resp2.StatusCode)
	}
}

func TestAdmin_CreateAgent_ReturnsTokenOnce(t *testing.T) {
	srv, ts := newTestServer(t)
	client, csrf := bootstrapAdminFlow(t, srv, ts)

	body := `{"name":"bot-1","kind":"agent","access_mode":"restrict_to_list","rules":[{"action":"allow","pattern":"/api/data/**","methods":["GET"]}]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/admin/identities", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := client.Do(req)
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
	if !strings.HasPrefix(tok.Token, "ab_") {
		t.Errorf("token prefix missing: %s", tok.Token)
	}

	// The token should work for GET (allowed) and fail for PUT (not in rules).
	getReq, _ := http.NewRequest("GET", ts.URL+"/api/data", nil)
	getReq.Header.Set("Authorization", "Bearer "+tok.Token)
	getResp, _ := http.DefaultClient.Do(getReq)
	if getResp.StatusCode != 200 {
		t.Errorf("GET with viewer token status = %d, want 200", getResp.StatusCode)
	}
	getResp.Body.Close()

	putReq, _ := http.NewRequest("PUT", ts.URL+"/api/data/foo", strings.NewReader(`"bar"`))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("Authorization", "Bearer "+tok.Token)
	putResp, _ := http.DefaultClient.Do(putReq)
	if putResp.StatusCode != 403 {
		t.Errorf("PUT with viewer token status = %d, want 403", putResp.StatusCode)
	}
	putResp.Body.Close()

	_ = srv
}

func TestAdmin_SessionCookie_NotSentToDataPlane(t *testing.T) {
	// The session cookie is scoped to /api/admin, so requests to /api/data
	// never receive it (browser behavior); the middleware for data endpoints
	// would ignore it anyway. Smoke-test that unauthenticated /api/data
	// returns 401 even after an admin has logged in.
	srv, ts := newTestServer(t)
	client, _ := bootstrapAdminFlow(t, srv, ts)

	req, _ := http.NewRequest("GET", ts.URL+"/api/data", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// No identities with a token → the server is NOT in open mode because
	// an admin identity exists. But there's no agent token either, so 401.
	if resp.StatusCode != 401 {
		t.Errorf("data endpoint should 401 without agent token, got %d", resp.StatusCode)
	}
}

func TestAdmin_Revoke_CannotRevokeSelf(t *testing.T) {
	srv, ts := newTestServer(t)
	client, csrf := bootstrapAdminFlow(t, srv, ts)

	// Look up the current admin's ID.
	req, _ := http.NewRequest("GET", ts.URL+"/api/admin/me", nil)
	resp, _ := client.Do(req)
	var me meResponse
	json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()

	req2, _ := http.NewRequest("POST", ts.URL+"/api/admin/identities/"+me.ID+"/revoke", nil)
	req2.Header.Set("X-CSRF-Token", csrf)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 400 {
		t.Errorf("self-revoke status = %d, want 400", resp2.StatusCode)
	}
	_ = srv
}

func TestAdmin_BootstrapCode_OneTime(t *testing.T) {
	srv, ts := newTestServer(t)
	code, _, err := srv.Auth.CreateBootstrapCode(60*60*1e9, "")
	if err != nil {
		t.Fatal(err)
	}

	// Use once.
	body1, _ := json.Marshal(setupRequest{Code: code, Name: "alice", Password: "correct-horse"})
	resp1, err := http.Post(ts.URL+"/api/admin/setup", "application/json", bytes.NewReader(body1))
	if err != nil {
		t.Fatal(err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != 201 {
		t.Fatalf("first setup failed: %d", resp1.StatusCode)
	}

	// Second use must fail.
	body2, _ := json.Marshal(setupRequest{Code: code, Name: "bob", Password: "correct-horse"})
	resp2, err := http.Post(ts.URL+"/api/admin/setup", "application/json", bytes.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 401 {
		t.Errorf("second use of code should be 401, got %d", resp2.StatusCode)
	}
}

func TestAdmin_Rotate_ReplacesToken(t *testing.T) {
	srv, ts := newTestServer(t)
	client, csrf := bootstrapAdminFlow(t, srv, ts)

	// Create an agent.
	body := strings.NewReader(`{"name":"bot-rotate","kind":"agent"}`)
	req, _ := http.NewRequest("POST", ts.URL+"/api/admin/identities", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var tok tokenResponse
	json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()
	oldToken := tok.Token

	// Rotate.
	req2, _ := http.NewRequest("POST", ts.URL+"/api/admin/identities/"+tok.ID+"/rotate", nil)
	req2.Header.Set("X-CSRF-Token", csrf)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	var rot tokenResponse
	json.NewDecoder(resp2.Body).Decode(&rot)
	resp2.Body.Close()
	if rot.Token == oldToken {
		t.Error("rotated token should differ")
	}

	// Old token should fail.
	gr1, _ := http.NewRequest("GET", ts.URL+"/api/data", nil)
	gr1.Header.Set("Authorization", "Bearer "+oldToken)
	r1, _ := http.DefaultClient.Do(gr1)
	r1.Body.Close()
	if r1.StatusCode != 401 {
		t.Errorf("old token status = %d, want 401", r1.StatusCode)
	}

	// New token should work.
	gr2, _ := http.NewRequest("GET", ts.URL+"/api/data", nil)
	gr2.Header.Set("Authorization", "Bearer "+rot.Token)
	r2, _ := http.DefaultClient.Do(gr2)
	r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Errorf("new token status = %d, want 200", r2.StatusCode)
	}
	_ = srv
}
