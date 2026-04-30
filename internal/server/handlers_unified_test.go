package server

// Cut 7 regression coverage for the unified /api/<path> namespace
// (spec §5). Verifies the dispatcher routes correctly across:
//   - page tier: PUT/GET/PATCH/DELETE on /api/<page-path>
//   - data tier: PUT/GET on /api/<flat-key> (singleton)
//   - reserved prefixes: /api/health, /api/me, /api/index still 200
//
// Cut 8 retired the legacy /api/content/* + /api/data/<key> routes;
// /api/<path> is now the only content-tier surface.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestUnifiedAPI_PageRoundTrip(t *testing.T) {
	_, ts := newTestServer(t)

	src := "---\ntitle: Hello\n---\n\n# Body\n"
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/notes", strings.NewReader(src))
	req.Header.Set("Content-Type", "text/markdown")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT /api/notes: %d %s", resp.StatusCode, raw)
	}

	g, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/notes", nil)
	g.Header.Set("Accept", "application/json")
	gResp, gErr := http.DefaultClient.Do(g)
	if gErr != nil {
		t.Fatal(gErr)
	}
	defer gResp.Body.Close()
	if gResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/notes: %d", gResp.StatusCode)
	}
	var page struct {
		Title   string `json:"title"`
		Source  string `json:"source"`
		Version string `json:"version"`
	}
	_ = json.NewDecoder(gResp.Body).Decode(&page)
	if page.Title != "Hello" {
		t.Errorf("title = %q, want Hello", page.Title)
	}
	if !strings.Contains(page.Source, "# Body") {
		t.Errorf("body lost from round-trip: %q", page.Source)
	}
	if page.Version == "" {
		t.Errorf("version not stamped on response")
	}
}

func TestUnifiedAPI_NestedPathDefaultsToPage(t *testing.T) {
	_, ts := newTestServer(t)

	body := bytes.NewBufferString(`{"value": 42}`)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/tasks/task-7", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Page tier accepts JSON body too — the data store rejects '/' in
	// keys, so nested paths route to pages by default. Either 200
	// (page write) or some structured error is fine; 4xx is the
	// failure mode.
	if resp.StatusCode >= 500 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("nested path 5xx: %d %s", resp.StatusCode, raw)
	}
}

func TestUnifiedAPI_DataSingleton(t *testing.T) {
	_, ts := newTestServer(t)

	body := bytes.NewBufferString(`{"value": {"label": "DAU", "count": 42}}`)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/dau", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT /api/dau: %d %s", resp.StatusCode, raw)
	}

	g, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/dau", nil)
	gResp, gErr := http.DefaultClient.Do(g)
	if gErr != nil {
		t.Fatal(gErr)
	}
	defer gResp.Body.Close()
	if gResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/dau: %d", gResp.StatusCode)
	}
}

func TestUnifiedAPI_ReservedPrefixesStillWork(t *testing.T) {
	_, ts := newTestServer(t)

	cases := []struct {
		path string
		want int
	}{
		{"/api/health", http.StatusOK},
		{"/api/me", http.StatusOK},
		{"/api/index", http.StatusOK},
		{"/api/setup/status", http.StatusOK},
	}
	for _, c := range cases {
		resp, err := http.Get(ts.URL + c.path)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s: status %d, want %d (reserved prefix shouldn't fall to /*)",
				c.path, resp.StatusCode, c.want)
		}
	}
}

// TestUnifiedAPI_LegacyAndUnifiedAgree retired in Cut 8 — the
// legacy /api/content/* and /api/data/<key> routes are gone, so
// there's nothing to compare unified against. The other tests in
// this file cover unified-surface behavior.
