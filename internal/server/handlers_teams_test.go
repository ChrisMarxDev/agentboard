package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/inbox"
	"github.com/christophermarx/agentboard/internal/teams"
)

func teamsCreate(slug string) teams.CreateParams {
	return teams.CreateParams{Slug: slug}
}

func teamsAdd(slug, username string) teams.AddMemberParams {
	return teams.AddMemberParams{Slug: slug, Username: username}
}

// waitForInbox polls until the recipient's inbox has at least `want`
// items, or times out. Needed because dispatchInboxNotifications runs
// in a goroutine.
func waitForInbox(srv *Server, recipient string, want int) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		items, err := srv.Inbox.List(inbox.ListParams{Recipient: recipient})
		if err != nil {
			return err
		}
		if len(items) >= want {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("inbox for %q did not reach %d items in time", recipient, want)
}

// Helpers ---------------------------------------------------------------

// adminClient returns an http.Client that sends requests as a newly-
// minted admin user. Needed because the default test client is an
// agent-kind user, and /api/admin/* requires admin.
func adminClient(t *testing.T, srv *Server) (*http.Client, string) {
	t.Helper()
	if _, err := srv.Auth.CreateUser(auth.CreateUserParams{
		Username: "team-admin",
		Kind:     auth.KindAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	tok, _ := auth.GenerateToken()
	if _, err := srv.Auth.CreateToken(auth.CreateTokenParams{
		Username:  "team-admin",
		TokenHash: auth.HashToken(tok),
	}); err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: &testAuthTransport{token: tok, inner: http.DefaultTransport}}, "team-admin"
}

// seedUser creates an agent-kind user with the given username.
func seedUser(t *testing.T, srv *Server, name string) {
	t.Helper()
	if _, err := srv.Auth.CreateUser(auth.CreateUserParams{
		Username: name,
		Kind:     auth.KindMember,
	}); err != nil {
		t.Fatalf("seed user %s: %v", name, err)
	}
}

// Tests -----------------------------------------------------------------

func TestTeamsCRUD(t *testing.T) {
	srv, ts := newTestServer(t)
	client, _ := adminClient(t, srv)

	// Seed users that will become members.
	seedUser(t, srv, "alice")
	seedUser(t, srv, "bob")

	// Create team with initial members.
	body, _ := json.Marshal(map[string]any{
		"slug":         "marketing",
		"display_name": "Marketing",
		"description":  "Comms and campaigns",
		"members":      []string{"alice", "bob"},
	})
	req, _ := http.NewRequest("POST", ts.URL+"/api/admin/teams", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Errorf("create status = %d", resp.StatusCode)
	}
	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created["slug"] != "marketing" {
		t.Errorf("slug = %v", created["slug"])
	}

	// GET single team.
	resp, err = http.Get(ts.URL + "/api/teams/marketing")
	if err != nil {
		t.Fatal(err)
	}
	var fetched map[string]any
	json.NewDecoder(resp.Body).Decode(&fetched)
	resp.Body.Close()
	members, _ := fetched["members"].([]any)
	if len(members) != 2 {
		t.Errorf("members len = %d", len(members))
	}

	// Reserved slug rejected.
	body2, _ := json.Marshal(map[string]string{"slug": "all"})
	req2, _ := http.NewRequest("POST", ts.URL+"/api/admin/teams", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := client.Do(req2)
	if resp2.StatusCode != 400 {
		t.Errorf("reserved slug status = %d, want 400", resp2.StatusCode)
	}
	resp2.Body.Close()

	// Slug collision with existing user.
	body3, _ := json.Marshal(map[string]string{"slug": "alice"})
	req3, _ := http.NewRequest("POST", ts.URL+"/api/admin/teams", bytes.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	resp3, _ := client.Do(req3)
	if resp3.StatusCode != 409 {
		t.Errorf("user-slug collision status = %d, want 409", resp3.StatusCode)
	}
	resp3.Body.Close()

	// Delete team.
	req4, _ := http.NewRequest("DELETE", ts.URL+"/api/admin/teams/marketing", nil)
	resp4, _ := client.Do(req4)
	if resp4.StatusCode != 204 {
		t.Errorf("delete status = %d", resp4.StatusCode)
	}
	resp4.Body.Close()
}

func TestTeamMemberOps(t *testing.T) {
	srv, ts := newTestServer(t)
	client, _ := adminClient(t, srv)

	seedUser(t, srv, "dana")
	seedUser(t, srv, "eve")

	// Create empty team.
	body, _ := json.Marshal(map[string]string{"slug": "design"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/admin/teams", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := client.Do(req)
	resp.Body.Close()

	// Add member.
	addBody, _ := json.Marshal(map[string]string{"username": "dana", "role": "lead"})
	addReq, _ := http.NewRequest("POST", ts.URL+"/api/admin/teams/design/members", bytes.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addResp, _ := client.Do(addReq)
	if addResp.StatusCode != 200 {
		t.Errorf("add status = %d", addResp.StatusCode)
	}
	addResp.Body.Close()

	// Add unknown user → 404.
	unknownBody, _ := json.Marshal(map[string]string{"username": "ghost"})
	unkReq, _ := http.NewRequest("POST", ts.URL+"/api/admin/teams/design/members", bytes.NewReader(unknownBody))
	unkReq.Header.Set("Content-Type", "application/json")
	unkResp, _ := client.Do(unkReq)
	if unkResp.StatusCode != 404 {
		t.Errorf("unknown user status = %d", unkResp.StatusCode)
	}
	unkResp.Body.Close()

	// Remove member.
	delReq, _ := http.NewRequest("DELETE", ts.URL+"/api/admin/teams/design/members/dana", nil)
	delResp, _ := client.Do(delReq)
	if delResp.StatusCode != 200 {
		t.Errorf("remove status = %d", delResp.StatusCode)
	}
	delResp.Body.Close()

	// Remove again → 404 (not member).
	del2Req, _ := http.NewRequest("DELETE", ts.URL+"/api/admin/teams/design/members/dana", nil)
	del2Resp, _ := client.Do(del2Req)
	if del2Resp.StatusCode != 404 {
		t.Errorf("re-remove status = %d", del2Resp.StatusCode)
	}
	del2Resp.Body.Close()
}

func TestTeamsRequireAdminForWrites(t *testing.T) {
	_, ts := newTestServer(t)
	// Default client is an agent user — writes must 403.
	body, _ := json.Marshal(map[string]string{"slug": "secret"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/admin/teams", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 403 {
		t.Errorf("agent on admin write status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestExpandMentionResolvesTeam(t *testing.T) {
	srv, _ := newTestServer(t)
	seedUser(t, srv, "alice")
	seedUser(t, srv, "bob")
	seedUser(t, srv, "charlie")

	if _, err := srv.Teams.Create(teamsCreate("ops")); err != nil {
		t.Fatal(err)
	}
	_ = srv.Teams.AddMember(teamsAdd("ops", "alice"))
	_ = srv.Teams.AddMember(teamsAdd("ops", "bob"))

	// User name wins over team name — "alice" is a user.
	got := srv.expandMention("alice")
	if len(got) != 1 || got[0] != "alice" {
		t.Errorf("user expansion = %v", got)
	}
	// Team name expands to members.
	got = srv.expandMention("ops")
	if len(got) != 2 {
		t.Errorf("team expansion = %v", got)
	}
	// Unknown → empty.
	if srv.expandMention("ghost") != nil {
		t.Errorf("unknown should be nil")
	}
	// @all expands to every active user.
	all := srv.expandMention("all")
	if len(all) < 3 {
		t.Errorf("@all expansion too small: %v", all)
	}
	// @agents expands to agent-kind users only.
	agents := srv.expandMention("agents")
	for _, u := range agents {
		if u == "team-admin" {
			t.Errorf("admin leaked into @agents: %v", agents)
		}
	}
}

func TestDispatchTeamMentionCreatesInboxItems(t *testing.T) {
	srv, _ := newTestServer(t)
	seedUser(t, srv, "alice")
	seedUser(t, srv, "bob")
	_, _ = srv.Teams.Create(teamsCreate("ops"))
	_ = srv.Teams.AddMember(teamsAdd("ops", "alice"))
	_ = srv.Teams.AddMember(teamsAdd("ops", "bob"))

	srv.dispatchInboxNotifications([]string{"ops"}, inbox.KindMention, "system", "/api/data/demo", "t1", "ops was pinged")

	// Dispatch is async; wait for both rows to appear.
	if err := waitForInbox(srv, "alice", 1); err != nil {
		t.Error(err)
	}
	if err := waitForInbox(srv, "bob", 1); err != nil {
		t.Error(err)
	}
}
