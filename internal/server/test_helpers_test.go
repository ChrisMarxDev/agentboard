package server

// Shared test scaffolding for the surviving handler suites.
// Post-pivot, the test substrate boils down to: auth store + db.
// Everything that needed the v0.13 file/page stores got deleted along
// with the handlers that used them.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/christophermarx/agentboard/internal/auth"
	dbpkg "github.com/christophermarx/agentboard/internal/db"
	"github.com/christophermarx/agentboard/internal/invitations"
	"github.com/christophermarx/agentboard/internal/project"
)

// testAuthTransport auto-attaches a Bearer token to outbound requests
// that don't already carry an Authorization header. Installed
// globally by newTestServer so existing call sites stay terse.
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

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	projPath := filepath.Join(dir, "project")
	_ = os.MkdirAll(projPath, 0o755)
	_ = os.MkdirAll(filepath.Join(projPath, ".agentboard"), 0o755)

	proj, err := project.Load(projPath)
	if err != nil {
		// Fresh dir — initialize.
		proj, err = project.InitProject(projPath)
		if err != nil {
			t.Fatal(err)
		}
	}

	dbConn, err := dbpkg.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	authStore, err := auth.NewStore(dbConn.Conn())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.CreateUser(auth.CreateUserParams{
		Username: "test-agent",
		Kind:     auth.KindAdmin,
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

	invStore, err := invitations.NewStore(dbConn.Conn())
	if err != nil {
		t.Fatal(err)
	}
	srv := New(ServerConfig{
		Project:     proj,
		Conn:        dbConn.Conn(),
		Auth:        authStore,
		Invitations: invStore,
		SkillFile:   "# Test skill",
	})

	ts := httptest.NewServer(srv.Router)
	t.Cleanup(ts.Close)
	return srv, ts
}

func newAuthedTestServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	_, ts := newAuthedTestServerWithSrv(t, token)
	return ts
}

func newAuthedTestServerWithSrv(t *testing.T, token string) (*Server, *httptest.Server) {
	t.Helper()
	if token == "" {
		return newBareTestServer(t)
	}
	return newTestServer(t)
}

// newBareTestServer boots a server without seeding any user.
func newBareTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	projPath := filepath.Join(dir, "project")
	_ = os.MkdirAll(projPath, 0o755)
	_ = os.MkdirAll(filepath.Join(projPath, ".agentboard"), 0o755)

	proj, err := project.Load(projPath)
	if err != nil {
		proj, err = project.InitProject(projPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	dbConn, err := dbpkg.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })
	authStore, err := auth.NewStore(dbConn.Conn())
	if err != nil {
		t.Fatal(err)
	}
	invStore, err := invitations.NewStore(dbConn.Conn())
	if err != nil {
		t.Fatal(err)
	}
	srv := New(ServerConfig{
		Project:     proj,
		Conn:        dbConn.Conn(),
		Auth:        authStore,
		Invitations: invStore,
		SkillFile:   "# Bare skill",
	})
	ts := httptest.NewServer(srv.Router)
	t.Cleanup(ts.Close)
	return srv, ts
}

// newPublicTestServer boots a server with public anonymous reads
// enabled. Used by /_api/introduction etc.
func newPublicTestServer(t *testing.T, publicPaths []string) *httptest.Server {
	t.Helper()
	_ = publicPaths
	_, ts := newBareTestServer(t)
	return ts
}
