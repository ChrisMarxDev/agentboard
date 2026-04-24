package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestIntroduction_DefaultIsMarkdown — no Accept → the agent-readable
// markdown primer, because that's the artifact most callers want.
func TestIntroduction_DefaultIsMarkdown(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/introduction", nil)
	req.Header.Del("Authorization")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Errorf("Content-Type = %q, want text/markdown", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "# AgentBoard") {
		t.Errorf("markdown missing top-level heading")
	}
	if !strings.Contains(s, "/api/content/") {
		t.Errorf("markdown missing API examples")
	}
}

// TestIntroduction_JSONOnRequest — Accept: application/json → structured.
func TestIntroduction_JSONOnRequest(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/introduction", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["product"] != "agentboard" {
		t.Errorf("product = %v, want agentboard", body["product"])
	}
}

// TestIntroduction_Markdown — Accept: text/markdown returns prose.
func TestIntroduction_Markdown(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/introduction", nil)
	req.Header.Set("Accept", "text/markdown")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Errorf("Content-Type = %q, want text/markdown", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "# AgentBoard") {
		t.Errorf("markdown missing expected heading")
	}
	if !strings.Contains(s, "/mcp") {
		t.Errorf("markdown missing MCP mention")
	}
}

// TestIntroduction_AccessibleWithoutAuth — the route must bypass the
// token middleware unconditionally. Even on a fresh board with zero
// users claimed, /introduction MUST answer 200.
func TestIntroduction_AccessibleWithoutAuth(t *testing.T) {
	// newPublicTestServer doesn't install the default token transport,
	// so this exercises the anonymous path directly.
	ts := newPublicTestServer(t, nil)
	resp, err := http.Get(ts.URL + "/introduction")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous GET /introduction: status = %d, want 200", resp.StatusCode)
	}
}
