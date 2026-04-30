package server

// Regression coverage for ISSUES.md #3 (initial PUT must succeed without
// If-Match) and ISSUES.md #4 (PATCH error message must match the parser's
// real accepted shape). Both surfaces — pages under /api/content/* and
// data under /api/data/* — go through the same checks. Spec §5: "A PUT
// to a path with no existing leaf MUST succeed without `If-Match`."

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestInitialPut_DataNoIfMatch — fresh /api/data/<key> writes succeed
// without any If-Match header AND without _meta.version in the body.
// Issue 3.
func TestInitialPut_DataNoIfMatch(t *testing.T) {
	_, ts := newTestServer(t)

	body := bytes.NewBufferString(`{"value": 42}`)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/data/never-existed", body)
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

	// Read it back, confirm the value landed.
	g, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/data/never-existed", nil)
	gResp, err := http.DefaultClient.Do(g)
	if err != nil {
		t.Fatal(err)
	}
	defer gResp.Body.Close()
	if gResp.StatusCode != http.StatusOK {
		t.Fatalf("GET after initial PUT: status=%d", gResp.StatusCode)
	}
	var got struct {
		Value json.RawMessage `json:"value"`
	}
	_ = json.NewDecoder(gResp.Body).Decode(&got)
	if string(got.Value) != "42" {
		t.Errorf("round-trip value: got %q want %q", got.Value, "42")
	}
}

// TestInitialPut_PageNoIfMatch — a fresh /api/content/<path> write
// without If-Match must succeed (no 412/409). Pages share the same
// rule via the unified `_meta.version` CAS in store.PageManager.
func TestInitialPut_PageNoIfMatch(t *testing.T) {
	_, ts := newTestServer(t)

	src := "---\ntitle: Fresh page\n---\n\n# Hello\n"
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/content/fresh", strings.NewReader(src))
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
	preq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/data/vet.clinic.status", put)
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
	preq2, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/data/vet.clinic.status", bad)
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
	var errBody struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &errBody); err != nil {
		t.Fatalf("decode err body: %v (raw=%s)", err, raw)
	}
	if !strings.Contains(errBody.Message, `{"value": <patch>}`) {
		t.Errorf("PATCH error must name the accepted shape; got: %s", errBody.Message)
	}
	if strings.Contains(errBody.Message, "top-level patch object") {
		t.Errorf("PATCH error still mentions the never-supported 'top-level patch object' shape: %s", errBody.Message)
	}

	// And the documented shape works.
	good := bytes.NewBufferString(`{"value": {"label": "Closed"}}`)
	preq3, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/data/vet.clinic.status", good)
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
