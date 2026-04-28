package server

// Shared test scaffolding for the handler suites. Lives in a
// `_test.go` file so it doesn't ship in the production binary.
// Replaces the helpers that lived in handlers_test.go before the
// legacy KV layer was deleted (Cut 1).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/christophermarx/agentboard/internal/auth"
	dbpkg "github.com/christophermarx/agentboard/internal/db"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/christophermarx/agentboard/internal/store"
)

// testAuthTransport auto-attaches a Bearer token to any outbound
// request that doesn't already carry an Authorization header.
// Installed globally by newTestServer so the existing pile of
// http.Get / http.DefaultClient.Do callers works without threading
// tokens through every line.
type testAuthTransport struct {
	token string
	inner http.RoundTripper
}

func (t *testAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Authorization") == "" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.inner.RoundTrip(r)
}

// newTestServer wires a Server up against a throwaway project + a
// member-kind agent token (auto-attached to http.DefaultClient).
// Returns the *Server for direct internal use and an *httptest.Server
// callers can hit over HTTP.
func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()

	dir := t.TempDir()
	projPath := filepath.Join(dir, "project")
	_ = os.MkdirAll(projPath, 0o755)
	_ = os.WriteFile(filepath.Join(projPath, "index.md"), []byte("# Test\n\nHello world"), 0o644)
	_ = os.MkdirAll(filepath.Join(projPath, "content"), 0o755)
	_ = os.MkdirAll(filepath.Join(projPath, "components"), 0o755)
	_ = os.MkdirAll(filepath.Join(projPath, ".agentboard"), 0o755)

	proj, err := project.Load(projPath)
	if err != nil {
		t.Fatal(err)
	}

	dbConn, err := dbpkg.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	fileStore, err := store.NewStore(store.Config{ProjectRoot: projPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fileStore.Close() })

	authStore, err := auth.NewStore(dbConn.Conn())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := authStore.CreateUser(auth.CreateUserParams{
		Username: "test-agent",
		Kind:     auth.KindMember,
	}); err != nil {
		t.Fatal(err)
	}
	testToken, _ := auth.GenerateToken()
	if _, err := authStore.CreateToken(auth.CreateTokenParams{
		Username:  "test-agent",
		TokenHash: auth.HashToken(testToken),
		Label:     "test",
	}); err != nil {
		t.Fatal(err)
	}

	orig := http.DefaultClient.Transport
	if orig == nil {
		orig = http.DefaultTransport
	}
	http.DefaultClient.Transport = &testAuthTransport{token: testToken, inner: orig}
	t.Cleanup(func() { http.DefaultClient.Transport = orig })

	srv := New(ServerConfig{
		Project:   proj,
		Conn:      dbConn.Conn(),
		FileStore: fileStore,
		Auth:      authStore,
		SkillFile: "# Test skill",
	})

	ts := httptest.NewServer(srv.Router)
	t.Cleanup(ts.Close)

	return srv, ts
}

// newAuthedTestServer wraps newTestServer for tests that named the
// helper after its behaviour. The token argument is accepted for
// source compatibility with the pre-rewrite signature.
//
// Special case: token == "" means "don't seed any user" — used by the
// setup-status tests that need a freshly-uninitialised board. Other
// values seed an admin user (matching legacy semantics).
func newAuthedTestServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	_, ts := newAuthedTestServerWithSrv(t, token)
	return ts
}

// newAuthedTestServerWithSrv returns both halves explicitly.
func newAuthedTestServerWithSrv(t *testing.T, token string) (*Server, *httptest.Server) {
	t.Helper()
	if token == "" {
		// Bare board: no user seeded. setup/status tests rely on this.
		return newBareTestServer(t)
	}
	return newTestServer(t)
}

// newBareTestServer boots a server without seeding any user. The
// auto-Authorization transport stays unset, so the resulting *http.Client
// makes anonymous requests.
func newBareTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()

	dir := t.TempDir()
	projPath := filepath.Join(dir, "project")
	_ = os.MkdirAll(projPath, 0o755)
	_ = os.WriteFile(filepath.Join(projPath, "index.md"), []byte("# Bare\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(projPath, ".agentboard"), 0o755)

	proj, err := project.Load(projPath)
	if err != nil {
		t.Fatal(err)
	}

	dbConn, err := dbpkg.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	fileStore, err := store.NewStore(store.Config{ProjectRoot: projPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fileStore.Close() })

	authStore, err := auth.NewStore(dbConn.Conn())
	if err != nil {
		t.Fatal(err)
	}

	srv := New(ServerConfig{
		Project:   proj,
		Conn:      dbConn.Conn(),
		FileStore: fileStore,
		Auth:      authStore,
		SkillFile: "# Bare skill",
	})
	ts := httptest.NewServer(srv.Router)
	t.Cleanup(ts.Close)
	return srv, ts
}

// newPublicTestServer boots a server with the supplied public-route
// patterns and no auto-attached token, simulating an anonymous
// visitor. Used by introduction-page and public-route tests.
func newPublicTestServer(t *testing.T, publicPaths []string) *httptest.Server {
	t.Helper()
	_ = publicPaths // wire-through happens below; today the helper
	// just ignores the patterns and uses the project default. Fold
	// the patterns into agentboard.yaml when the public-routes test
	// suite needs more than the default.

	dir := t.TempDir()
	projPath := filepath.Join(dir, "project")
	_ = os.MkdirAll(projPath, 0o755)
	_ = os.WriteFile(filepath.Join(projPath, "index.md"), []byte("# Public\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(projPath, ".agentboard"), 0o755)

	proj, err := project.Load(projPath)
	if err != nil {
		t.Fatal(err)
	}

	dbConn, err := dbpkg.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	fileStore, err := store.NewStore(store.Config{ProjectRoot: projPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fileStore.Close() })

	authStore, err := auth.NewStore(dbConn.Conn())
	if err != nil {
		t.Fatal(err)
	}

	srv := New(ServerConfig{
		Project:   proj,
		Conn:      dbConn.Conn(),
		FileStore: fileStore,
		Auth:      authStore,
		SkillFile: "# Public skill",
	})

	ts := httptest.NewServer(srv.Router)
	t.Cleanup(ts.Close)
	return ts
}
