package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readPageFromDisk reads the raw .md file written by the PATCH handler.
// We bypass GET /api/content/* because that endpoint returns the body
// only — frontmatter is stripped during scan and travels in the JSON
// payload's `frontmatter` map. For PATCH-correctness assertions we want
// the byte-exact assembled file.
func readPageFromDisk(t *testing.T, srv *Server, path string) string {
	t.Helper()
	full := filepath.Join(srv.Project.ContentDir(), path+".md")
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// patchPage sends a JSON patch body. Returns the response.
func patchPage(t *testing.T, base, path string, payload any, ifMatch string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPatch, base+"/api/"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if ifMatch != "" {
		req.Header.Set("If-Match", `"`+ifMatch+`"`)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch %s: %v", path, err)
	}
	return resp
}

// TestPatchPage_FrontmatterMerge — the canonical kanban flow.
// PATCH a card to flip its `col` and the rest of the frontmatter +
// body survive verbatim.
func TestPatchPage_FrontmatterMerge(t *testing.T) {
	srv, ts := newTestServer(t)

	seedPage(t, srv, "tasks/card-1", `---
title: Build it
col: todo
priority: 2
owner: chris
---

# Body stays put

Multi-line prose remains.
`)

	resp := patchPage(t, ts.URL, "tasks/card-1", map[string]any{
		"frontmatter_patch": map[string]any{"col": "done"},
	}, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("patch: %d %s", resp.StatusCode, body)
	}

	source := readPageFromDisk(t, srv, "tasks/card-1")
	_ = ts
	if !strings.Contains(source, "col: done") {
		t.Errorf("col not flipped:\n%s", source)
	}
	if !strings.Contains(source, "priority: 2") {
		t.Errorf("untouched field lost:\n%s", source)
	}
	if !strings.Contains(source, "Multi-line prose remains.") {
		t.Errorf("body lost:\n%s", source)
	}
}

// TestPatchPage_NullDeletesKey — RFC-7396 semantics: null in the patch
// removes the field from frontmatter.
func TestPatchPage_NullDeletesKey(t *testing.T) {
	srv, ts := newTestServer(t)
	seedPage(t, srv, "n", "---\ntitle: Hi\nstale: true\n---\n\n# body\n")

	resp := patchPage(t, ts.URL, "n", map[string]any{
		"frontmatter_patch": map[string]any{"stale": nil},
	}, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status: %d", resp.StatusCode)
	}
	source := readPageFromDisk(t, srv, "n")
	_ = ts
	if strings.Contains(source, "stale:") {
		t.Errorf("expected stale removed, got:\n%s", source)
	}
	if !strings.Contains(source, "title: Hi") {
		t.Errorf("expected title preserved")
	}
}

// TestPatchPage_BodyReplace — body replacement leaves frontmatter
// alone.
func TestPatchPage_BodyReplace(t *testing.T) {
	srv, ts := newTestServer(t)
	seedPage(t, srv, "b", "---\ntitle: Keep\n---\n\n# old body\n")

	newBody := "# brand new body\n"
	resp := patchPage(t, ts.URL, "b", map[string]any{
		"body": newBody,
	}, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status: %d", resp.StatusCode)
	}
	source := readPageFromDisk(t, srv, "b")
	_ = ts
	if !strings.Contains(source, "title: Keep") {
		t.Errorf("frontmatter lost:\n%s", source)
	}
	if !strings.Contains(source, "brand new body") {
		t.Errorf("body not replaced:\n%s", source)
	}
	if strings.Contains(source, "old body") {
		t.Errorf("old body still present:\n%s", source)
	}
}

// TestPatchPage_IfMatchStale — wrong If-Match returns 412 with the
// current etag in the body so the caller can re-base.
func TestPatchPage_IfMatchStale(t *testing.T) {
	srv, ts := newTestServer(t)
	seedPage(t, srv, "s", "---\ntitle: A\n---\n\nbody\n")

	resp := patchPage(t, ts.URL, "s", map[string]any{
		"frontmatter_patch": map[string]any{"col": "done"},
	}, "deadbeefdeadbeef")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("expected 412, got %d", resp.StatusCode)
	}
	var payload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if payload["code"] != "STALE_WRITE" {
		t.Errorf("code = %v, want STALE_WRITE", payload["code"])
	}
}

// TestPatchPage_NotFound — patching a missing path returns 404 (we do
// not auto-create on PATCH; clients should PUT for that).
func TestPatchPage_NotFound(t *testing.T) {
	_, ts := newTestServer(t)
	resp := patchPage(t, ts.URL, "does/not/exist", map[string]any{
		"frontmatter_patch": map[string]any{"x": 1},
	}, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// TestPatchPage_EmptyPatch — neither field set should 400.
func TestPatchPage_EmptyPatch(t *testing.T) {
	srv, ts := newTestServer(t)
	seedPage(t, srv, "e", "---\ntitle: A\n---\nbody\n")
	resp := patchPage(t, ts.URL, "e", map[string]any{}, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
