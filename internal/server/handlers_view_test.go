package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestView_AuthedOpen — a bearer-carrying admin POSTs view/open and
// gets back a bundle with source + data keys scoped to the page AST.
func TestView_AuthedOpen(t *testing.T) {
	_, ts := newTestServer(t)

	// Seed a page that references one data key.
	seed := bytes.NewBufferString(`# Hello

<Metric source="counter.value" />
`)
	wr, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/content/hello", seed)
	wr.Header.Set("Content-Type", "text/markdown")
	seedResp, _ := http.DefaultClient.Do(wr)
	seedResp.Body.Close()

	// Seed counter.value through the file-store v2 endpoint. The
	// envelope wraps the value; the broker unwraps before bundling.
	dr, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v2/data/counter.value", strings.NewReader(`{"value":42}`))
	dr.Header.Set("Content-Type", "application/json")
	drResp, _ := http.DefaultClient.Do(dr)
	drResp.Body.Close()

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
	if !strings.Contains(bundle.Source, "counter.value") {
		t.Errorf("source missing the metric reference")
	}
	if v, ok := bundle.Data["counter.value"]; !ok || v != float64(42) {
		t.Errorf("data[counter.value] = %v, want 42", v)
	}
}

// TestView_AnonymousOnPrivateIs401 — an anonymous visitor hitting
// view/open for a page that isn't in public.paths → 401.
func TestView_AnonymousOnPrivateIs401(t *testing.T) {
	_, ts := newTestServer(t)

	// Seed a page.
	seed := bytes.NewBufferString(`# Private`)
	wr, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/content/private", seed)
	wr.Header.Set("Content-Type", "text/markdown")
	seedResp, _ := http.DefaultClient.Do(wr)
	seedResp.Body.Close()

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
	_, ts := newTestServer(t)

	// Seed a page.
	seed := bytes.NewBufferString(`# Shareme
<Metric source="share.demo" />`)
	wr, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/content/shareme", seed)
	wr.Header.Set("Content-Type", "text/markdown")
	seedResp, _ := http.DefaultClient.Do(wr)
	seedResp.Body.Close()
	// Seed the referenced data — v2 endpoint expects an envelope.
	dr, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v2/data/share.demo", strings.NewReader(`{"value":"ok"}`))
	dr.Header.Set("Content-Type", "application/json")
	drResp, _ := http.DefaultClient.Do(dr)
	drResp.Body.Close()

	// Mint a share.
	mintBody, _ := json.Marshal(map[string]any{"path": "/shareme"})
	mintReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/share", bytes.NewBuffer(mintBody))
	mintReq.Header.Set("Content-Type", "application/json")
	mintResp, _ := http.DefaultClient.Do(mintReq)
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
	if _, ok := b.Data["share.demo"]; !ok {
		t.Errorf("share visitor missing resolved data key")
	}

	// Cookie → /api/data/* must 401.
	dreq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v2/data/share.demo", nil)
	dresp, err := bare.Do(dreq)
	if err != nil {
		t.Fatal(err)
	}
	dresp.Body.Close()
	if dresp.StatusCode != http.StatusUnauthorized {
		t.Errorf("cookie direct data: status = %d, want 401", dresp.StatusCode)
	}

	// Cookie → write must 401.
	wreq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v2/data/share.demo", strings.NewReader(`"x"`))
	wresp, err := bare.Do(wreq)
	if err != nil {
		t.Fatal(err)
	}
	wresp.Body.Close()
	if wresp.StatusCode != http.StatusUnauthorized {
		t.Errorf("cookie write: status = %d, want 401", wresp.StatusCode)
	}
}

// TestView_CookieReAnchors — a share cookie issued for /shareme cannot
// request /otherpath; the broker re-anchors to the share's scoped path.
func TestView_CookieReAnchors(t *testing.T) {
	_, ts := newTestServer(t)

	// Two pages.
	for _, p := range []string{"shareme", "other"} {
		seed := bytes.NewBufferString("# " + p)
		wr, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/content/"+p, seed)
		wr.Header.Set("Content-Type", "text/markdown")
		wresp, _ := http.DefaultClient.Do(wr)
		wresp.Body.Close()
	}

	// Mint share for /shareme.
	mintBody, _ := json.Marshal(map[string]any{"path": "/shareme"})
	mintReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/share", bytes.NewBuffer(mintBody))
	mintReq.Header.Set("Content-Type", "application/json")
	mintResp, _ := http.DefaultClient.Do(mintReq)
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
	_, ts := newTestServer(t)

	// Seed + mint + redeem.
	seed := bytes.NewBufferString(`# x`)
	wr, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/content/revoketest", seed)
	wr.Header.Set("Content-Type", "text/markdown")
	wresp, _ := http.DefaultClient.Do(wr)
	wresp.Body.Close()

	mintBody, _ := json.Marshal(map[string]any{"path": "/revoketest"})
	mintReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/share", bytes.NewBuffer(mintBody))
	mintReq.Header.Set("Content-Type", "application/json")
	mintResp, _ := http.DefaultClient.Do(mintReq)
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
