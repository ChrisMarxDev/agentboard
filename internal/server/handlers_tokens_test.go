package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/christophermarx/agentboard/internal/auth"
)

// Token endpoints live at /api/users/{username}/tokens/* with the
// self-or-admin scope rule. These tests cover the full matrix:
//
//   admin vs any target              → allowed
//   member vs self                   → allowed
//   member vs another member         → 403
//   member vs bot                    → 403
//   bot vs self                      → allowed (same as member)
//
// Plus the self-revoke-in-use guard.

func seedUserKind(t *testing.T, srv *Server, name string, kind auth.Kind) string {
	t.Helper()
	if _, err := srv.Auth.CreateUser(auth.CreateUserParams{
		Username: name,
		Kind:     kind,
	}); err != nil {
		t.Fatal(err)
	}
	tok, _ := auth.GenerateToken()
	if _, err := srv.Auth.CreateToken(auth.CreateTokenParams{
		Username:  name,
		TokenHash: auth.HashToken(tok),
		Label:     "t1",
	}); err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestTokens_AdminCanMintForAny(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	_ = seedUserKind(t, srv, "dana", auth.KindMember)
	r, err := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/dana/tokens", `{"label":"ci"}`, adminToken))
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 201 {
		t.Errorf("admin minting for other = %d, want 201", r.StatusCode)
	}
	r.Body.Close()
}

func TestTokens_MemberCanMintForSelf(t *testing.T) {
	srv, ts := newTestServer(t)
	daneToken := seedUserKind(t, srv, "dana", auth.KindMember)
	r, err := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/dana/tokens", `{"label":"laptop"}`, daneToken))
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 201 {
		t.Errorf("self-mint = %d, want 201", r.StatusCode)
	}
	r.Body.Close()
}

func TestTokens_MemberCannotMintForOther(t *testing.T) {
	srv, ts := newTestServer(t)
	daneToken := seedUserKind(t, srv, "dana", auth.KindMember)
	_ = seedUserKind(t, srv, "eve", auth.KindMember)
	r, err := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/eve/tokens", `{"label":"laptop"}`, daneToken))
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 403 {
		t.Errorf("member→other = %d, want 403", r.StatusCode)
	}
	r.Body.Close()
}

func TestTokens_MemberCannotMintForBot(t *testing.T) {
	srv, ts := newTestServer(t)
	daneToken := seedUserKind(t, srv, "dana", auth.KindMember)
	_ = seedUserKind(t, srv, "helper", auth.KindBot)
	r, err := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/helper/tokens", `{"label":"ci"}`, daneToken))
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 403 {
		t.Errorf("member→bot = %d, want 403", r.StatusCode)
	}
	r.Body.Close()
}

func TestTokens_AdminMintsForBot(t *testing.T) {
	srv, ts := newTestServer(t)
	adminToken := seedAdmin(t, srv)
	_ = seedUserKind(t, srv, "helper", auth.KindBot)
	r, err := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/helper/tokens", `{"label":"ci"}`, adminToken))
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 201 {
		t.Errorf("admin→bot = %d, want 201", r.StatusCode)
	}
	r.Body.Close()
}

func TestTokens_BotMintsForSelf(t *testing.T) {
	srv, ts := newTestServer(t)
	botToken := seedUserKind(t, srv, "helper", auth.KindBot)
	r, err := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/helper/tokens", `{"label":"second"}`, botToken))
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 201 {
		t.Errorf("bot→self = %d, want 201", r.StatusCode)
	}
	r.Body.Close()
}

func TestTokens_SelfRevokeInUseRejected(t *testing.T) {
	srv, ts := newTestServer(t)
	daneToken := seedUserKind(t, srv, "dana", auth.KindMember)
	// Find the ID of the token she's authenticating with.
	tokens, _ := srv.Auth.ListTokensForUser("dana")
	if len(tokens) == 0 {
		t.Fatal("expected dana to have a token")
	}
	tokenID := tokens[0].ID
	r, err := http.DefaultClient.Do(authReq(t, "POST",
		ts.URL+"/api/users/dana/tokens/"+tokenID+"/revoke", "", daneToken))
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 400 {
		t.Errorf("self-revoke-in-use = %d, want 400", r.StatusCode)
	}
	r.Body.Close()
}

func TestTokens_ListForSelf(t *testing.T) {
	srv, ts := newTestServer(t)
	daneToken := seedUserKind(t, srv, "dana", auth.KindMember)
	r, err := http.DefaultClient.Do(authReq(t, "GET",
		ts.URL+"/api/users/dana/tokens", "", daneToken))
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Tokens []struct{ ID string } `json:"tokens"`
	}
	json.NewDecoder(r.Body).Decode(&resp)
	r.Body.Close()
	if len(resp.Tokens) == 0 {
		t.Error("expected at least one token")
	}
}

func TestMe_ReturnsSignedInUser(t *testing.T) {
	_, ts := newTestServer(t)
	// Default http.Client transport auto-injects the seeded test-agent
	// token, so a bare Get hits /api/me authenticated.
	r, err := http.Get(ts.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("/api/me = %d", r.StatusCode)
	}
	var me struct {
		Username string `json:"username"`
		Kind     string `json:"kind"`
	}
	json.NewDecoder(r.Body).Decode(&me)
	if me.Username == "" {
		t.Errorf("/api/me returned empty user: %+v", me)
	}
}
