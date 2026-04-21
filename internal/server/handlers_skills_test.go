package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// writeProjectSkill drops a SKILL.md (and optional extras) under the test
// project's files/skills/<slug>/ folder.
func writeProjectSkill(t *testing.T, projPath, slug string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(projPath, "files", "skills", slug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSkillsList_Empty(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/skills")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 skills, got %d", len(list))
	}
}

func TestSkillsList_ReturnsValidSkills(t *testing.T) {
	srv, ts := newTestServer(t)
	writeProjectSkill(t, srv.Project.Path, "greeter", map[string]string{
		"SKILL.md": "---\nname: greeter\ndescription: Greet people\n---\n",
	})
	// Folder without a manifest should be ignored.
	os.MkdirAll(filepath.Join(srv.Project.Path, "files", "skills", "ignored"), 0755)

	resp, err := http.Get(ts.URL + "/api/skills")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(list))
	}
	if list[0]["slug"] != "greeter" {
		t.Errorf("slug = %v, want greeter", list[0]["slug"])
	}
	if list[0]["description"] != "Greet people" {
		t.Errorf("description = %v, want 'Greet people'", list[0]["description"])
	}
}

func TestSkillsGet_Zip(t *testing.T) {
	srv, ts := newTestServer(t)
	writeProjectSkill(t, srv.Project.Path, "zippy", map[string]string{
		"SKILL.md":    "---\nname: zippy\ndescription: zipped skill\n---\n\nBody\n",
		"examples.md": "# examples\n",
	})

	resp, err := http.Get(ts.URL + "/api/skills/zippy")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	got := map[string]bool{}
	for _, f := range zr.File {
		got[f.Name] = true
	}
	for _, want := range []string{"zippy/SKILL.md", "zippy/examples.md"} {
		if !got[want] {
			t.Errorf("zip missing %q (has: %v)", want, got)
		}
	}
}

func TestSkillsGet_NotFound(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/skills/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestSkillsGet_InvalidSlug(t *testing.T) {
	_, ts := newTestServer(t)
	// chi's router normalizes paths, so '/' in the slug wouldn't reach this
	// handler as a single param. Test the backslash-rejection path instead.
	resp, err := http.Get(ts.URL + "/api/skills/bad%5Cslug")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
