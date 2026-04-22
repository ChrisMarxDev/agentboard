package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestIfMatch_Data_FreshWriteReturnsEtag verifies that a write echoes an
// ETag header and the response body carries updated_at.
func TestIfMatch_Data_FreshWriteReturnsEtag(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := doJSON(ts.URL+"/api/data/k", http.MethodPut, `"hello"`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Error("expected ETag header on write response")
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["updated_at"] == nil {
		t.Error("expected updated_at in body")
	}
}

// TestIfMatch_Data_StaleReturns412 is the canonical optimistic-concurrency
// test: two writers race, the loser gets 412 with the winner's state.
func TestIfMatch_Data_StaleReturns412(t *testing.T) {
	_, ts := newTestServer(t)

	// Writer A seeds and captures updated_at.
	r1, _ := doJSON(ts.URL+"/api/data/k", http.MethodPut, `1`, nil)
	r1.Body.Close()
	g, _ := http.Get(ts.URL + "/api/data/k")
	var meta map[string]any
	_ = json.NewDecoder(g.Body).Decode(&meta)
	g.Body.Close()
	expectedTS, _ := meta["updated_at"].(string)
	if expectedTS == "" {
		t.Fatal("missing updated_at")
	}

	// Writer B writes without If-Match — succeeds, bumps updated_at.
	r2, _ := doJSON(ts.URL+"/api/data/k", http.MethodPut, `2`, nil)
	r2.Body.Close()

	// Writer A retries with their stale expected → 412 with current state.
	r3, err := doJSON(ts.URL+"/api/data/k", http.MethodPut, `99`, http.Header{
		"If-Match": []string{`"` + expectedTS + `"`},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Body.Close()
	if r3.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("want 412, got %d", r3.StatusCode)
	}
	var stale map[string]any
	_ = json.NewDecoder(r3.Body).Decode(&stale)
	if stale["code"] != "STALE_WRITE" {
		t.Errorf("want code=STALE_WRITE, got %v", stale["code"])
	}
	if stale["current"] == nil {
		t.Error("expected current state in 412 body")
	}

	// Value on disk should be 2 (writer B's), never 99.
	g2, _ := http.Get(ts.URL + "/api/data/k")
	var final map[string]any
	_ = json.NewDecoder(g2.Body).Decode(&final)
	g2.Body.Close()
	if v, ok := final["value"].(float64); !ok || v != 2 {
		t.Errorf("want value=2, got %v", final["value"])
	}
}

// TestIfMatch_Page_ContentAddressedEtag verifies pages GET emits an etag
// computed from source, and that a stale write gets 412.
func TestIfMatch_Page_ContentAddressedEtag(t *testing.T) {
	_, ts := newTestServer(t)

	// Seed a page.
	w1, _ := doJSON(ts.URL+"/api/content/features/test", http.MethodPut, "# Test\n\nBody", map[string][]string{
		"Content-Type": {"text/markdown"},
	})
	if w1.StatusCode != 200 {
		t.Fatalf("seed status=%d", w1.StatusCode)
	}
	w1.Body.Close()

	// Read etag.
	g, _ := http.Get(ts.URL + "/api/content/features/test")
	etag := g.Header.Get("ETag")
	g.Body.Close()
	if etag == "" {
		t.Fatal("expected ETag on page GET")
	}

	// Concurrent write bumps etag.
	w2, _ := doJSON(ts.URL+"/api/content/features/test", http.MethodPut, "# Test\n\nConcurrent change", map[string][]string{
		"Content-Type": {"text/markdown"},
	})
	w2.Body.Close()

	// Stale write → 412.
	w3, _ := doJSON(ts.URL+"/api/content/features/test", http.MethodPut, "# Test\n\nLosing write", map[string][]string{
		"Content-Type": {"text/markdown"},
		"If-Match":     {etag},
	})
	defer w3.Body.Close()
	if w3.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("want 412, got %d", w3.StatusCode)
	}
	var stale map[string]any
	_ = json.NewDecoder(w3.Body).Decode(&stale)
	if stale["code"] != "STALE_WRITE" {
		t.Errorf("want STALE_WRITE, got %v", stale["code"])
	}
}

// doJSON is a small helper that does a request with the given method + body
// and returns the response. The global test transport attaches the bearer
// token.
func doJSON(url, method, body string, hdr http.Header) (*http.Response, error) {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	if hdr != nil {
		for k, v := range hdr {
			req.Header[k] = v
		}
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(req)
}

// Compile-time guard that io.ReadAll + bytes stay imported where tests
// might extend later.
var _ = io.ReadAll
var _ = bytes.NewReader
