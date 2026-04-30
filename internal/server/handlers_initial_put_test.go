package server

// Regression coverage for ISSUES.md #3 (initial PUT must succeed without
// If-Match) and ISSUES.md #4 (PATCH error message must match the parser's
// real accepted shape). Spec §5: "A PUT to a path with no existing leaf
// MUST succeed without `If-Match`."
//
// Post-§14: every write lands as a page leaf, so the JSON `{"value":…}`
// envelope shape that used to write a singleton now writes a page with
// `value:` in the frontmatter.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestInitialPut_DataNoIfMatch — fresh PUT with `{"value":…}` body
// succeeds without If-Match and lands as a page with `value:` in the
// frontmatter (per §14, every write is a page leaf).
func TestInitialPut_DataNoIfMatch(t *testing.T) {
	_, ts := newTestServer(t)

	body := bytes.NewBufferString(`{"value": 42}`)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/never-existed", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT initial: status=%d body=%s", resp.StatusCode, raw)
	}

	// Read it back; the value lives in the page's frontmatter.
	g, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/never-existed", nil)
	gResp, err := http.DefaultClient.Do(g)
	if err != nil {
		t.Fatal(err)
	}
	defer gResp.Body.Close()
	if gResp.StatusCode != http.StatusOK {
		t.Fatalf("GET after initial PUT: status=%d", gResp.StatusCode)
	}
	var got struct {
		Frontmatter map[string]any `json:"frontmatter"`
	}
	_ = json.NewDecoder(gResp.Body).Decode(&got)
	if v, ok := got.Frontmatter["value"]; !ok || v != float64(42) {
		t.Errorf("round-trip frontmatter.value: got %v want 42", v)
	}
}

// TestInitialPut_PageNoIfMatch — a fresh /api/content/<path> write
// without If-Match must succeed (no 412/409). Pages share the same
// rule via the unified `_meta.version` CAS in store.PageManager.
func TestInitialPut_PageNoIfMatch(t *testing.T) {
	_, ts := newTestServer(t)

	src := "---\ntitle: Fresh page\n---\n\n# Hello\n"
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/fresh", strings.NewReader(src))
	req.Header.Set("Content-Type", "text/markdown")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT initial page: status=%d body=%s", resp.StatusCode, raw)
	}

	// Confirm a server-stamped `_meta.version` came back as the etag.
	if etag := resp.Header.Get("ETag"); etag == "" {
		t.Errorf("missing ETag header on initial page write")
	}
}

// TestPatchData_ErrorMessageMatchesShape — Issue 4. The PATCH parser
// only accepts `{"value": <patch>}`; its old error message also named
// "(or top-level patch object)" which never worked. The new message
// names exactly the shape that succeeds.
func TestPatchData_ErrorMessageMatchesShape(t *testing.T) {
	_, ts := newTestServer(t)

	// Seed a key so PATCH has something to merge against.
	put := bytes.NewBufferString(`{"value": {"label": "Open"}}`)
	preq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/vet.clinic.status", put)
	preq.Header.Set("Content-Type", "application/json")
	pResp, err := http.DefaultClient.Do(preq)
	if err != nil {
		t.Fatal(err)
	}
	pResp.Body.Close()
	if pResp.StatusCode != http.StatusOK {
		t.Fatalf("seed: status=%d", pResp.StatusCode)
	}

	// Wrong shape → parser rejects → error message must match reality.
	bad := bytes.NewBufferString(`{"detail": {"label": "Open"}}`)
	preq2, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/vet.clinic.status", bad)
	preq2.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(preq2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PATCH wrong shape: status=%d (want 400)", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	// Post-§14 the PATCH parser names the page-shaped fields plus the
	// translated `{"value": <patch>}` envelope. Either reference is
	// enough — both are accepted.
	if !strings.Contains(body, `frontmatter_patch`) && !strings.Contains(body, `{\"value\": <patch>}`) {
		t.Errorf("PATCH error must name an accepted shape; got: %s", body)
	}
	if strings.Contains(body, "top-level patch object") {
		t.Errorf("PATCH error still mentions the never-supported 'top-level patch object' shape: %s", body)
	}

	// And the documented shape works.
	good := bytes.NewBufferString(`{"value": {"label": "Closed"}}`)
	preq3, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/vet.clinic.status", good)
	preq3.Header.Set("Content-Type", "application/json")
	resp3, err := http.DefaultClient.Do(preq3)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp3.Body)
		t.Fatalf("PATCH documented shape: status=%d body=%s", resp3.StatusCode, raw)
	}
}
