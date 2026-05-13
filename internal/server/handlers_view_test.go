package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/christophermarx/agentboard/internal/auth"
)

// TestView_AuthedOpen — a bearer-carrying admin POSTs view/open and
// gets back a bundle with source + same-page frontmatter resolved.
// Per CORE_GUIDELINES §14, source bindings resolve against the
// rendering page's own frontmatter (or folder collections), never
// against another page's scalars.
func TestView_AuthedOpen(t *testing.T) {
	srv, ts := newTestServer(t)

	// Seed a page whose frontmatter holds the metric value its body
	// references. Direct PageManager write — the v0.13 REST PUT
	// surface was retired in the git-substrate pivot.
	seedPage(t, srv, "hello", `---
counter_value: 42
---
# Hello

<Metric source="counter_value" />
`)

	// view/open as admin.
	body, _ := json.Marshal(map[string]any{"path": "hello"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/view/open", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("view/open: status = %d, body = %s", resp.StatusCode, raw)
	}
	var bundle struct {
		Authority string         `json:"authority"`
		Source    string         `json:"source"`
		Data      map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Authority != "admin" && bundle.Authority != "agent" {
		t.Errorf("authority = %q, want admin or agent", bundle.Authority)
	}
	if !strings.Contains(bundle.Source, "counter_value") {
		t.Errorf("source missing the metric reference")
	}
	if v, ok := bundle.Data["counter_value"]; !ok || v != float64(42) {
		t.Errorf("data[counter_value] = %v, want 42", v)
	}
}

// TestView_SessionCookieAccepted — a browser-session cookie minted
// via /api/auth/login authenticates /api/view/open the same way a
// bearer token does. Regression guard: before this landed, the view
// broker only knew about bearer + share-cookie + anonymous, so a
// cookie-logged-in user got 401 on every page open and bounced to
// /login?reason=unauthorized.
func TestView_SessionCookieAccepted(t *testing.T) {
	srv, ts := newTestServer(t)
	seedUserWithPassword(t, srv, "alice", "view-cookie-pw-1234", auth.KindAdmin)

	// Seed a page.
	seedPage(t, srv, "hello", `# Hello

<Metric source="counter.value" />
`)

	// Log in via /api/auth/login with a cookie jar — same shape the
	// SPA uses.
	c := loginClient(t)
	loginAs(t, ts.URL, c, "alice", "view-cookie-pw-1234").Body.Close()

	// view/open with the session cookie. No bearer header.
	body, _ := json.Marshal(map[string]any{"path": "hello"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/view/open", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("cookie-authed view/open: status = %d, body = %s", resp.StatusCode, raw)
	}
	var bundle struct {
		Authority string `json:"authority"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&bundle)
	if bundle.Authority != "admin" && bundle.Authority != "agent" {
		t.Errorf("authority = %q, want admin or agent (NOT share or anonymous)", bundle.Authority)
	}
}

// TestView_AnonymousOnPrivateIs401 — an anonymous visitor hitting
// view/open for a page that isn't in public.paths → 401.
func TestView_AnonymousOnPrivateIs401(t *testing.T) {
	srv, ts := newTestServer(t)

	// Seed a page.
	seedPage(t, srv, "private", `# Private`)

	// Bare client (no default token transport).
	bare := &http.Client{}
	body, _ := json.Marshal(map[string]any{"path": "private"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/view/open", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := bare.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon view/open private: status = %d, want 401", resp.StatusCode)
	}
}

// TestView_RedeemCookieFlow — mint share, redeem to cookie, bare
// client with the cookie can view/open; cookie cannot read /api/data/*.
func TestView_RedeemCookieFlow(t *testing.T) {
	srv, ts := newTestServer(t)

	// Seed a page whose own frontmatter holds the metric value.
	// (Per CORE_GUIDELINES §14, source bindings resolve against the
	// rendering page's frontmatter, not a sibling singleton.)
	seedPage(t, srv, "shareme", `---
share_demo: ok
---
# Shareme
<Metric source="share_demo" />`)

	// Mint a share.
	mintBody, _ := json.Marshal(map[string]any{"path": "/shareme"})
	mintReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/share", bytes.NewBuffer(mintBody))
	mintReq.Header.Set("Content-Type", "application/json")
	mintResp, mintErr := http.DefaultClient.Do(mintReq)
	if mintErr != nil {
		t.Fatal(mintErr)
	}
	defer mintResp.Body.Close()
	var created struct {
		Token string `json:"token"`
		ID    string `json:"id"`
	}
	_ = json.NewDecoder(mintResp.Body).Decode(&created)
	if created.Token == "" {
		t.Fatalf("share create: no token")
	}

	// Redeem with a bare client — captures cookies in a jar.
	jar, _ := newCookieJar()
	bare := &http.Client{Jar: jar}
	rbody, _ := json.Marshal(map[string]any{"token": created.Token})
	rreq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/share/redeem", bytes.NewBuffer(rbody))
	rreq.Header.Set("Content-Type", "application/json")
	rresp, err := bare.Do(rreq)
	if err != nil {
		t.Fatal(err)
	}
	rresp.Body.Close()
	if rresp.StatusCode != 200 {
		t.Fatalf("redeem: status = %d", rresp.StatusCode)
	}

	// view/open with cookie.
	obody, _ := json.Marshal(map[string]any{"path": "shareme"})
	oreq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/view/open", bytes.NewBuffer(obody))
	oreq.Header.Set("Content-Type", "application/json")
	oresp, err := bare.Do(oreq)
	if err != nil {
		t.Fatal(err)
	}
	defer oresp.Body.Close()
	if oresp.StatusCode != 200 {
		raw, _ := io.ReadAll(oresp.Body)
		t.Fatalf("cookie view/open: status = %d, body = %s", oresp.StatusCode, raw)
	}
	var b struct {
		Authority string         `json:"authority"`
		Data      map[string]any `json:"data"`
	}
	_ = json.NewDecoder(oresp.Body).Decode(&b)
	if b.Authority != "share" {
		t.Errorf("authority = %q, want share", b.Authority)
	}
	if _, ok := b.Data["share_demo"]; !ok {
		t.Errorf("share visitor missing resolved frontmatter field")
	}

	// Cookie → bare /api/<other-page> must 401.
	dreq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/other", nil)
	dresp, err := bare.Do(dreq)
	if err != nil {
		t.Fatal(err)
	}
	dresp.Body.Close()
	if dresp.StatusCode != http.StatusUnauthorized {
		t.Errorf("cookie direct read: status = %d, want 401", dresp.StatusCode)
	}

	// (The legacy "cookie → PUT /api/<path>" check used to live here.
	// That endpoint was retired in the git-substrate pivot; cookie
	// writes against the workspace go through git push and inherit
	// repo-level ACLs, not the HTTP middleware. No replacement test
	// needed here — the cookie-can't-read check above already proves
	// the cookie's scope is bounded.)
}

// TestView_CookieReAnchors — a share cookie issued for /shareme cannot
// request /otherpath; the broker re-anchors to the share's scoped path.
func TestView_CookieReAnchors(t *testing.T) {
	srv, ts := newTestServer(t)

	// Two pages.
	for _, p := range []string{"shareme", "other"} {
		seedPage(t, srv, p, "# "+p)
	}

	// Mint share for /shareme.
	mintBody, _ := json.Marshal(map[string]any{"path": "/shareme"})
	mintReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/share", bytes.NewBuffer(mintBody))
	mintReq.Header.Set("Content-Type", "application/json")
	mintResp, mintErr := http.DefaultClient.Do(mintReq)
	if mintErr != nil {
		t.Fatal(mintErr)
	}
	defer mintResp.Body.Close()
	var created struct{ Token string }
	_ = json.NewDecoder(mintResp.Body).Decode(&created)

	// Redeem into cookie.
	jar, _ := newCookieJar()
	bare := &http.Client{Jar: jar}
	rbody, _ := json.Marshal(map[string]any{"token": created.Token})
	rreq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/share/redeem", bytes.NewBuffer(rbody))
	rreq.Header.Set("Content-Type", "application/json")
	rresp, _ := bare.Do(rreq)
	rresp.Body.Close()

	// Try to open /other with the cookie — the handler re-anchors.
	obody, _ := json.Marshal(map[string]any{"path": "other"})
	oreq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/view/open", bytes.NewBuffer(obody))
	oreq.Header.Set("Content-Type", "application/json")
	oresp, err := bare.Do(oreq)
	if err != nil {
		t.Fatal(err)
	}
	defer oresp.Body.Close()
	var b struct {
		Path  string `json:"path"`
		Title string `json:"title"`
	}
	_ = json.NewDecoder(oresp.Body).Decode(&b)
	if b.Path != "shareme" {
		t.Errorf("cookie re-anchor: got path=%q, want shareme", b.Path)
	}
}

// TestView_RevokeCascadesToSession — revoking the share token deletes
// the cookie session (FK cascade). The cookie then stops working.
func TestView_RevokeCascadesToSession(t *testing.T) {
	srv, ts := newTestServer(t)

	// Seed + mint + redeem.
	seedPage(t, srv, "revoketest", `# x`)

	mintBody, _ := json.Marshal(map[string]any{"path": "/revoketest"})
	mintReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/share", bytes.NewBuffer(mintBody))
	mintReq.Header.Set("Content-Type", "application/json")
	mintResp, mintErr := http.DefaultClient.Do(mintReq)
	if mintErr != nil {
		t.Fatal(mintErr)
	}
	defer mintResp.Body.Close()
	var created struct {
		Token string `json:"token"`
		ID    string `json:"id"`
	}
	_ = json.NewDecoder(mintResp.Body).Decode(&created)

	jar, _ := newCookieJar()
	bare := &http.Client{Jar: jar}
	rbody, _ := json.Marshal(map[string]any{"token": created.Token})
	rreq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/share/redeem", bytes.NewBuffer(rbody))
	rreq.Header.Set("Content-Type", "application/json")
	rresp, _ := bare.Do(rreq)
	rresp.Body.Close()

	// Session works.
	obody, _ := json.Marshal(map[string]any{"path": "revoketest"})
	oreq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/view/open", bytes.NewBuffer(obody))
	oreq.Header.Set("Content-Type", "application/json")
	oresp, _ := bare.Do(oreq)
	oresp.Body.Close()
	if oresp.StatusCode != 200 {
		t.Fatalf("pre-revoke: %d", oresp.StatusCode)
	}

	// Revoke via admin.
	drev, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/share/"+created.ID, nil)
	drevResp, _ := http.DefaultClient.Do(drev)
	drevResp.Body.Close()

	// Cookie now useless.
	o2, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/view/open", bytes.NewBuffer(obody))
	o2.Header.Set("Content-Type", "application/json")
	o2resp, _ := bare.Do(o2)
	o2resp.Body.Close()
	if o2resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("post-revoke: status = %d, want 401", o2resp.StatusCode)
	}
}
