package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/data"
	"github.com/christophermarx/agentboard/internal/project"
)

// newPublicTestServer is a variant of newTestServer that seeds an
// agentboard.yaml with a `public.paths` config and does NOT install the
// default token transport — callers need to exercise both anonymous
// and authenticated request paths.
func newPublicTestServer(t *testing.T, publicPaths []string) *httptest.Server {
	t.Helper()

	dir := t.TempDir()
	projPath := filepath.Join(dir, "project")
	if err := os.MkdirAll(projPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projPath, "index.md"), []byte("# Hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Author a yaml config with the requested public paths.
	yaml := "title: test\n"
	if len(publicPaths) > 0 {
		yaml += "public:\n  paths:\n"
		for _, p := range publicPaths {
			yaml += "    - " + p + "\n"
		}
	}
	if err := os.WriteFile(filepath.Join(projPath, "agentboard.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	proj, err := project.Load(projPath)
	if err != nil {
		t.Fatal(err)
	}

	store, err := data.NewSQLiteStore(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	authStore, err := auth.NewStore(store.DB())
	if err != nil {
		t.Fatal(err)
	}

	srv := New(ServerConfig{
		Project:   proj,
		Store:     store,
		Auth:      authStore,
		SkillFile: "# Test skill",
	})

	ts := httptest.NewServer(srv.Router)
	t.Cleanup(ts.Close)
	return ts
}

// TestPublicRoutes_AnonymousReadAllowed verifies that a GET to a path
// matching a public pattern succeeds without a bearer token.
func TestPublicRoutes_AnonymousReadAllowed(t *testing.T) {
	ts := newPublicTestServer(t, []string{"/api/content/**", "/api/content"})

	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/content", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("anonymous GET /api/content: status = %d, body = %s", resp.StatusCode, body)
	}
}

// TestPublicRoutes_AnonymousWriteForbidden confirms the
// writes_require_auth invariant — even if the path would otherwise
// be publicly readable, a write goes through auth and gets 401'd.
func TestPublicRoutes_AnonymousWriteForbidden(t *testing.T) {
	ts := newPublicTestServer(t, []string{"/api/content/**"})

	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/content/foo", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("anonymous PUT /api/content/foo: status = %d, want 401, body = %s", resp.StatusCode, body)
	}
}

// TestPublicRoutes_NonMatchingPath401 confirms paths NOT in the
// config still require auth.
func TestPublicRoutes_NonMatchingPath401(t *testing.T) {
	ts := newPublicTestServer(t, []string{"/api/content/skills/**"})

	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/data", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("anonymous GET /api/data: status = %d, want 401, body = %s", resp.StatusCode, body)
	}
}

// TestPublicRoutes_AdminPathAlwaysProtected ensures that even a
// matcher that would cover /api/admin/* cannot expose it.
func TestPublicRoutes_AdminPathAlwaysProtected(t *testing.T) {
	ts := newPublicTestServer(t, []string{"/api/**"})

	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/admin/tokens", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("anonymous GET /api/admin/tokens: status = %d, want 401, body = %s", resp.StatusCode, body)
	}
}

// TestPublicRoutes_EmptyConfigNothingPublic verifies a zero config
// means full protection — every API route requires auth.
func TestPublicRoutes_EmptyConfigNothingPublic(t *testing.T) {
	ts := newPublicTestServer(t, nil)

	for _, path := range []string{"/api/content", "/api/data", "/api/components"} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without token: status = %d, want 401", path, resp.StatusCode)
		}
	}
}
