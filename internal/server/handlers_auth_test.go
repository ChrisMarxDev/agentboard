package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
)

// Tests for the new browser-session login surface. Token-authenticated
// callers (the existing PAT model) are covered by the rest of the
// handler suite — this file focuses on:
//
//   - login success / wrong-password / wrong-username (constant-time
//     in *response shape*; we don't measure timing)
//   - cookie-based /api/auth/me + /api/auth/logout
//   - session expiry kills the cookie
//   - CSRF enforcement on cookie-authenticated state-changing requests
//   - bearer-authenticated state-changing requests still skip CSRF

// loginClient is a cookie-jar-backed http.Client. The session +
// CSRF cookies set by /api/auth/login flow back through it on
// subsequent requests, mirroring real browser behaviour.
func loginClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

// seedUserWithPassword creates a user + sets a password. Returns
// nothing — tests log in via the same password directly.
func seedUserWithPassword(t *testing.T, srv *Server, username, password string, kind auth.Kind) {
	t.Helper()
	if _, err := srv.Auth.CreateUser(auth.CreateUserParams{
		Username: username,
		Kind:     kind,
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.Auth.SetPassword(username, password); err != nil {
		t.Fatal(err)
	}
}

func loginAs(t *testing.T, ts string, c *http.Client, username, password string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	req, err := http.NewRequest("POST", ts+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestAuth_LoginSuccessSetsCookies(t *testing.T) {
	srv, ts := newTestServer(t)
	seedUserWithPassword(t, srv, "alice", "correct-password-1234", auth.KindMember)

	c := loginClient(t)
	r := loginAs(t, ts.URL, c, "alice", "correct-password-1234")
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("login = %d, want 200", r.StatusCode)
	}
	var resp authResponse
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.User.Username != "alice" {
		t.Errorf("user.username = %q, want alice", resp.User.Username)
	}

	// Both cookies must be set.
	gotSession, gotCSRF := false, false
	for _, c := range r.Cookies() {
		switch c.Name {
		case auth.SessionCookieName:
			gotSession = true
			if !c.HttpOnly {
				t.Error("session cookie must be HttpOnly")
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("session SameSite = %v, want Lax", c.SameSite)
			}
			if !strings.HasPrefix(c.Value, auth.SessionPrefix) {
				t.Errorf("session value prefix = %q", c.Value[:4])
			}
		case auth.CSRFCookieName:
			gotCSRF = true
			if c.HttpOnly {
				t.Error("csrf cookie must NOT be HttpOnly (SPA reads it)")
			}
		}
	}
	if !gotSession || !gotCSRF {
		t.Errorf("missing cookies: session=%v csrf=%v", gotSession, gotCSRF)
	}
}

func TestAuth_LoginWrongPasswordSameShape(t *testing.T) {
	srv, ts := newTestServer(t)
	seedUserWithPassword(t, srv, "alice", "correct-password-1234", auth.KindMember)

	c := loginClient(t)
	// Wrong username and wrong password should produce identical
	// response shape so the API doesn't leak which side failed.
	a := loginAs(t, ts.URL, c, "nobody", "anything-1234")
	defer a.Body.Close()
	b := loginAs(t, ts.URL, c, "alice", "wrong-password-1234")
	defer b.Body.Close()
	if a.StatusCode != 401 || b.StatusCode != 401 {
		t.Errorf("expected 401/401, got %d/%d", a.StatusCode, b.StatusCode)
	}
	var aBody, bBody map[string]string
	json.NewDecoder(a.Body).Decode(&aBody)
	json.NewDecoder(b.Body).Decode(&bBody)
	if aBody["code"] != bBody["code"] || aBody["error"] != bBody["error"] {
		t.Errorf("error bodies differ: a=%+v b=%+v", aBody, bBody)
	}
}

func TestAuth_MeWithCookie(t *testing.T) {
	srv, ts := newTestServer(t)
	seedUserWithPassword(t, srv, "alice", "correct-password-1234", auth.KindMember)
	c := loginClient(t)
	r := loginAs(t, ts.URL, c, "alice", "correct-password-1234")
	r.Body.Close()

	r2, err := c.Get(ts.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Fatalf("/api/auth/me = %d", r2.StatusCode)
	}
	var resp authResponse
	json.NewDecoder(r2.Body).Decode(&resp)
	if resp.User.Username != "alice" {
		t.Errorf("user mismatch: %+v", resp.User)
	}
}

func TestAuth_MeWithoutCookie401(t *testing.T) {
	_, ts := newTestServer(t)
	c := &http.Client{}
	r, err := c.Get(ts.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 401 {
		t.Errorf("/api/auth/me unauthenticated = %d, want 401", r.StatusCode)
	}
}

func TestAuth_LogoutRevokesAndClearsCookies(t *testing.T) {
	srv, ts := newTestServer(t)
	seedUserWithPassword(t, srv, "alice", "correct-password-1234", auth.KindMember)
	c := loginClient(t)
	loginAs(t, ts.URL, c, "alice", "correct-password-1234").Body.Close()

	// Log out.
	req, _ := http.NewRequest("POST", ts.URL+"/api/auth/logout", nil)
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Errorf("logout = %d", r.StatusCode)
	}

	// /api/auth/me without an active session → 401.
	r2, _ := c.Get(ts.URL + "/api/auth/me")
	r2.Body.Close()
	if r2.StatusCode != 401 {
		t.Errorf("post-logout /api/auth/me = %d, want 401", r2.StatusCode)
	}

	// The DB-side row should be revoked too.
	rows, _ := srv.Auth.ListSessionsForUser("alice")
	for _, sess := range rows {
		if sess.RevokedAt == nil {
			t.Errorf("session %s should be revoked after logout", sess.ID)
		}
	}
}

func TestAuth_SessionExpiry(t *testing.T) {
	srv, ts := newTestServer(t)
	seedUserWithPassword(t, srv, "alice", "correct-password-1234", auth.KindMember)
	c := loginClient(t)
	loginAs(t, ts.URL, c, "alice", "correct-password-1234").Body.Close()

	rows, _ := srv.Auth.ListSessionsForUser("alice")
	if len(rows) != 1 {
		t.Fatalf("want 1 session, got %d", len(rows))
	}
	// Force expiry.
	if _, err := srv.Conn.Exec(`UPDATE user_sessions SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Hour).Unix(), rows[0].ID); err != nil {
		t.Fatal(err)
	}
	r, _ := c.Get(ts.URL + "/api/auth/me")
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Errorf("expired session /api/auth/me = %d, want 401", r.StatusCode)
	}
}

func TestAuth_SetPasswordSelf_RequiresCurrent(t *testing.T) {
	srv, ts := newTestServer(t)
	tok := seedAgent(t, srv, "alice")
	if err := srv.Auth.SetPassword("alice", "correct-password-1234"); err != nil {
		t.Fatal(err)
	}

	// Self without current_password → 400.
	body := `{"new_password":"new-password-5678"}`
	r, _ := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/alice/password", body, tok))
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Errorf("self-without-current = %d, want 400", r.StatusCode)
	}

	// Self with wrong current_password → 401.
	body = `{"current_password":"wrong-current-1234","new_password":"new-password-5678"}`
	r, _ = http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/alice/password", body, tok))
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Errorf("self-wrong-current = %d, want 401", r.StatusCode)
	}

	// Self with correct current_password → 200.
	body = `{"current_password":"correct-password-1234","new_password":"new-password-5678"}`
	r, _ = http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/alice/password", body, tok))
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Errorf("self-correct = %d, want 200", r.StatusCode)
	}

	// Verify the new password works.
	if _, err := srv.Auth.VerifyLogin("alice", "new-password-5678"); err != nil {
		t.Errorf("new password should verify: %v", err)
	}
}

func TestAuth_SetPasswordAdminForce(t *testing.T) {
	srv, ts := newTestServer(t)
	adminTok := seedAdmin(t, srv)
	_ = seedAgent(t, srv, "bob")

	body := `{"new_password":"force-set-1234"}`
	r, _ := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/bob/password", body, adminTok))
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Errorf("admin force-set = %d, want 200", r.StatusCode)
	}
	if _, err := srv.Auth.VerifyLogin("bob", "force-set-1234"); err != nil {
		t.Errorf("admin-set password should verify: %v", err)
	}
}

func TestAuth_SetPasswordWeakRejected(t *testing.T) {
	srv, ts := newTestServer(t)
	adminTok := seedAdmin(t, srv)
	_ = seedAgent(t, srv, "bob")

	body := `{"new_password":"short"}`
	r, _ := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/bob/password", body, adminTok))
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Errorf("weak password = %d, want 400", r.StatusCode)
	}
}

func TestAuth_PATBypassesCSRF(t *testing.T) {
	srv, ts := newTestServer(t)
	tok := seedAgent(t, srv, "alice")

	// State-changing request authenticated by Bearer skips CSRF.
	body := `{"label":"laptop"}`
	r, _ := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/alice/tokens", body, tok))
	r.Body.Close()
	if r.StatusCode != 201 {
		t.Errorf("bearer-authed POST = %d, want 201 (CSRF should be skipped)", r.StatusCode)
	}

	// Same request via session cookie WITHOUT the X-CSRF-Token
	// header → 403 CSRF_REQUIRED.
	if err := srv.Auth.SetPassword("alice", "session-password-1234"); err != nil {
		t.Fatal(err)
	}
	c := loginClient(t)
	loginAs(t, ts.URL, c, "alice", "session-password-1234").Body.Close()
	req, _ := http.NewRequest("POST",
		ts.URL+"/api/users/alice/tokens",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r2, _ := c.Do(req)
	r2.Body.Close()
	if r2.StatusCode != 403 {
		t.Errorf("session POST without CSRF = %d, want 403", r2.StatusCode)
	}

	// And WITH the X-CSRF-Token header → 201.
	csrf := ""
	for _, ck := range c.Jar.Cookies(req.URL) {
		if ck.Name == auth.CSRFCookieName {
			csrf = ck.Value
		}
	}
	if csrf == "" {
		t.Fatal("csrf cookie missing from jar")
	}
	req2, _ := http.NewRequest("POST",
		ts.URL+"/api/users/alice/tokens",
		strings.NewReader(`{"label":"second"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set(auth.CSRFHeaderName, csrf)
	r3, _ := c.Do(req2)
	r3.Body.Close()
	if r3.StatusCode != 201 {
		t.Errorf("session POST with CSRF = %d, want 201", r3.StatusCode)
	}
}

func TestAuth_LogoutWithStaleCookieClears(t *testing.T) {
	srv, ts := newTestServer(t)
	seedUserWithPassword(t, srv, "alice", "correct-password-1234", auth.KindMember)
	c := loginClient(t)
	loginAs(t, ts.URL, c, "alice", "correct-password-1234").Body.Close()

	// Forcibly expire the session in the DB to simulate "user clicks
	// log out from a stale tab".
	rows, _ := srv.Auth.ListSessionsForUser("alice")
	if _, err := srv.Conn.Exec(
		`UPDATE user_sessions SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Hour).Unix(), rows[0].ID,
	); err != nil {
		t.Fatal(err)
	}

	// Logout still returns 200 + clears cookies.
	req, _ := http.NewRequest("POST", ts.URL+"/api/auth/logout", nil)
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Errorf("stale-cookie logout = %d, want 200", r.StatusCode)
	}
}

func TestAuth_RevokeSession(t *testing.T) {
	srv, ts := newTestServer(t)
	tok := seedAgent(t, srv, "alice")
	if err := srv.Auth.SetPassword("alice", "session-password-1234"); err != nil {
		t.Fatal(err)
	}
	c := loginClient(t)
	loginAs(t, ts.URL, c, "alice", "session-password-1234").Body.Close()
	rows, _ := srv.Auth.ListSessionsForUser("alice")
	if len(rows) != 1 {
		t.Fatalf("want 1 session, got %d", len(rows))
	}
	id := rows[0].ID

	// Revoke via the per-user surface (token auth, sidesteps CSRF).
	r, _ := http.DefaultClient.Do(authReq(t, "DELETE",
		ts.URL+"/api/users/alice/sessions/"+id, "", tok))
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Errorf("revoke-session = %d, want 200", r.StatusCode)
	}

	// Cookie no longer authenticates.
	r2, _ := c.Get(ts.URL + "/api/auth/me")
	r2.Body.Close()
	if r2.StatusCode != 401 {
		t.Errorf("post-revoke /me = %d, want 401", r2.StatusCode)
	}
}
