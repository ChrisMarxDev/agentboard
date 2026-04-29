package server

// End-to-end coverage for the in-process OAuth 2.1 / MCP authorization
// surface (RFC 9728, RFC 8414, RFC 7591). Driven through the same
// httptest harness as the rest of the handler suite — what arrives
// here arrives over real HTTP, including the chi router and middleware
// stack.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/christophermarx/agentboard/internal/auth"
)

func TestOAuth_Discovery(t *testing.T) {
	_, ts := newTestServer(t)
	base := ts.URL

	// Both well-known docs MUST be reachable without authentication —
	// a client doesn't have a token yet when it's asking how to get one.
	cases := []struct {
		path     string
		mustHave []string
	}{
		{"/.well-known/oauth-protected-resource", []string{"resource", "authorization_servers", "bearer_methods_supported"}},
		{"/.well-known/oauth-authorization-server", []string{"issuer", "authorization_endpoint", "token_endpoint", "registration_endpoint", "code_challenge_methods_supported"}},
	}
	for _, c := range cases {
		req, _ := http.NewRequest("GET", base+c.path, nil)
		// Clear the auto-attached Bearer to prove anonymous access works.
		req.Header.Set("Authorization", "")
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: HTTP %d", c.path, resp.StatusCode)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("%s: decode: %v", c.path, err)
		}
		for _, k := range c.mustHave {
			if _, ok := body[k]; !ok {
				t.Errorf("%s: missing key %q in %v", c.path, k, body)
			}
		}
	}
}

func TestOAuth_UnauthenticatedMCP_HasBearerChallenge(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "") // suppress auto-attached test token
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	got := resp.Header.Get("WWW-Authenticate")
	if !strings.Contains(got, `Bearer realm="AgentBoard"`) {
		t.Errorf("missing Bearer realm in WWW-Authenticate: %q", got)
	}
	if !strings.Contains(got, `resource_metadata="`) {
		t.Errorf("missing resource_metadata in WWW-Authenticate: %q", got)
	}
}

func TestOAuth_FullFlow_PKCE_AuthorizationCode(t *testing.T) {
	srv, ts := newTestServer(t)

	// Mint a fresh PAT for the test user so we can drive the consent
	// form authentication step (the /authorize page asks the user for
	// their AgentBoard token).
	userToken := mintFreshTokenFor(t, srv, "test-agent")

	// 1. DCR.
	clientID := dcrRegister(t, ts, "Connector Test")

	// 2. PKCE pair.
	verifier, challenge := pkcePair()

	// 3. /oauth/authorize render.
	authURL := ts.URL + "/oauth/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {"https://example.test/cb"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {"mcp"},
		"state":                 {"abc"},
		"resource":              {ts.URL + "/mcp"},
	}.Encode()
	page := httpGetNoFollow(t, authURL)
	if page.StatusCode != 200 {
		t.Fatalf("authorize page: %d", page.StatusCode)
	}

	// 4. /oauth/authorize/decide with token + Allow.
	form := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {"https://example.test/cb"},
		"state":                 {"abc"},
		"scope":                 {"mcp"},
		"resource":              {ts.URL + "/mcp"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"decision":              {"allow"},
		"token":                 {userToken},
	}
	resp := httpPostFormNoFollow(t, ts.URL+"/oauth/authorize/decide", form)
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("decide: expected 302, got %d: %s", resp.StatusCode, body)
	}
	loc := resp.Header.Get("Location")
	parsed, _ := url.Parse(loc)
	code := parsed.Query().Get("code")
	if code == "" {
		t.Fatalf("decide redirect missing code: %s", loc)
	}
	if got := parsed.Query().Get("state"); got != "abc" {
		t.Errorf("state echo: got %q, want abc", got)
	}

	// 5. /oauth/token exchange.
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"client_id":     {clientID},
		"redirect_uri":  {"https://example.test/cb"},
		"resource":      {ts.URL + "/mcp"},
	}
	tokenJSON := postTokenEndpoint(t, ts, tokenForm)
	access := tokenJSON["access_token"].(string)
	refresh := tokenJSON["refresh_token"].(string)
	if !strings.HasPrefix(access, auth.OAuthAccessPrefix) {
		t.Errorf("access_token prefix: %q", access)
	}
	if !strings.HasPrefix(refresh, auth.OAuthRefreshPrefix) {
		t.Errorf("refresh_token prefix: %q", refresh)
	}

	// 6. Use access token at /mcp.
	req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	r2, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Errorf("MCP with oat_ token: HTTP %d", r2.StatusCode)
	}

	// 7. Same token MUST be rejected outside the MCP audience.
	req2, _ := http.NewRequest("GET", ts.URL+"/api/me", nil)
	req2.Header.Set("Authorization", "Bearer "+access)
	r3, err := http.DefaultTransport.RoundTrip(req2)
	if err != nil {
		t.Fatal(err)
	}
	r3.Body.Close()
	if r3.StatusCode != http.StatusUnauthorized {
		t.Errorf("audience scoping: /api/me with oat_ should be 401, got %d", r3.StatusCode)
	}

	// 8. Refresh rotation.
	refreshForm := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {clientID},
	}
	rotated := postTokenEndpoint(t, ts, refreshForm)
	if rotated["access_token"] == "" || rotated["refresh_token"] == "" {
		t.Errorf("refresh: missing tokens in response %v", rotated)
	}
	if rotated["refresh_token"].(string) == refresh {
		t.Errorf("refresh: token must rotate on use")
	}

	// 9. Old refresh token is now invalid (single-use).
	stale := httpPostFormNoFollow(t, ts.URL+"/oauth/token", refreshForm)
	if stale.StatusCode != http.StatusBadRequest {
		t.Errorf("old refresh after rotation: expected 400, got %d", stale.StatusCode)
	}
}

func TestOAuth_PKCE_WrongVerifier_Rejects(t *testing.T) {
	srv, ts := newTestServer(t)
	userToken := mintFreshTokenFor(t, srv, "test-agent")
	clientID := dcrRegister(t, ts, "PKCE Test")

	verifier, challenge := pkcePair()
	code := runAuthorizeUntilCode(t, ts, clientID, "https://example.test/cb", challenge, userToken)

	// Swap the verifier with a fresh one — token endpoint MUST reject.
	wrong, _ := pkcePair()
	_ = verifier // original; intentionally not used to redeem

	resp := httpPostFormNoFollow(t, ts.URL+"/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {wrong},
		"client_id":     {clientID},
		"redirect_uri":  {"https://example.test/cb"},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong verifier: expected 400, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "invalid_grant" {
		t.Errorf("expected invalid_grant, got %v", body["error"])
	}
}

func TestOAuth_AuthorizationCode_SingleUse(t *testing.T) {
	srv, ts := newTestServer(t)
	userToken := mintFreshTokenFor(t, srv, "test-agent")
	clientID := dcrRegister(t, ts, "Single-use Test")

	verifier, challenge := pkcePair()
	code := runAuthorizeUntilCode(t, ts, clientID, "https://example.test/cb", challenge, userToken)

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"client_id":     {clientID},
		"redirect_uri":  {"https://example.test/cb"},
	}
	first := postTokenEndpoint(t, ts, form)
	if first["access_token"] == "" {
		t.Fatalf("first redemption should succeed: %v", first)
	}
	// Replay the same code — MUST be rejected.
	replay := httpPostFormNoFollow(t, ts.URL+"/oauth/token", form)
	if replay.StatusCode != http.StatusBadRequest {
		t.Errorf("code replay: expected 400, got %d", replay.StatusCode)
	}
}

func TestOAuth_Authorize_RejectsBadResource(t *testing.T) {
	srv, ts := newTestServer(t)
	_ = mintFreshTokenFor(t, srv, "test-agent")
	clientID := dcrRegister(t, ts, "Audience Test")
	_, challenge := pkcePair()

	authURL := ts.URL + "/oauth/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {"https://example.test/cb"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"resource":              {"https://malicious.example/elsewhere"},
		"state":                 {"x"},
	}.Encode()
	resp := httpGetNoFollow(t, authURL)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 redirect with error, got %d", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Query().Get("error") != "invalid_target" {
		t.Errorf("expected error=invalid_target, got %q", loc.Query().Get("error"))
	}
}

func TestOAuth_DCR_RejectsNonHTTPSRedirect(t *testing.T) {
	_, ts := newTestServer(t)
	body := strings.NewReader(`{"client_name":"x","redirect_uris":["http://attacker.example/cb"]}`)
	req, _ := http.NewRequest("POST", ts.URL+"/oauth/register", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-https redirect_uri, got %d", resp.StatusCode)
	}
}

// ------------------------------------------------------------------
// helpers
// ------------------------------------------------------------------

func mintFreshTokenFor(t *testing.T, srv *Server, username string) string {
	t.Helper()
	plain, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Auth.CreateToken(auth.CreateTokenParams{
		Username:  username,
		TokenHash: auth.HashToken(plain),
		Label:     "oauth-test",
	}); err != nil {
		t.Fatal(err)
	}
	return plain
}

func dcrRegister(t *testing.T, ts *httptest.Server, name string) string {
	t.Helper()
	body := strings.NewReader(`{"client_name":"` + name + `","redirect_uris":["https://example.test/cb"],"grant_types":["authorization_code","refresh_token"],"token_endpoint_auth_method":"none"}`)
	req, _ := http.NewRequest("POST", ts.URL+"/oauth/register", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("DCR: HTTP %d: %s", resp.StatusCode, b)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	id, ok := out["client_id"].(string)
	if !ok {
		t.Fatalf("DCR: missing client_id in %v", out)
	}
	return id
}

func pkcePair() (verifier, challenge string) {
	// 32-byte URL-safe random verifier per RFC 7636 §4.1. Crypto-random
	// so two calls in the same test never collide (the wrong-verifier
	// test relies on the second call producing a *different* value).
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return
}

func runAuthorizeUntilCode(t *testing.T, ts *httptest.Server, clientID, redirectURI, challenge, userToken string) string {
	t.Helper()
	form := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {"s"},
		"scope":                 {"mcp"},
		"resource":              {ts.URL + "/mcp"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"decision":              {"allow"},
		"token":                 {userToken},
	}
	resp := httpPostFormNoFollow(t, ts.URL+"/oauth/authorize/decide", form)
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize decide: %d: %s", resp.StatusCode, body)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in redirect: %s", resp.Header.Get("Location"))
	}
	return code
}

func postTokenEndpoint(t *testing.T, ts *httptest.Server, form url.Values) map[string]any {
	t.Helper()
	resp := httpPostFormNoFollow(t, ts.URL+"/oauth/token", form)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("token endpoint: HTTP %d: %s", resp.StatusCode, b)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// noRedirectClient is used for OAuth flow tests where we want to
// inspect the 302 Location ourselves rather than auto-follow.
func noRedirectClient() *http.Client {
	return &http.Client{
		Transport: http.DefaultTransport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func httpGetNoFollow(t *testing.T, url string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func httpPostFormNoFollow(t *testing.T, url string, form url.Values) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
