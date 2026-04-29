package server

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
)

// OAuth 2.1 / RFC 7591 / RFC 9728 endpoints, served alongside the MCP
// resource so a single AgentBoard binary functions as both the
// authorization server and the protected resource. Wired into
// server.buildRouter outside the gated group — these routes MUST be
// reachable without a token (the whole point is to acquire one).

// ---------------- /oauth/register ----------------

// dcrRequest is the RFC 7591 §3.1 client metadata accepted by DCR.
// We pick a deliberately small subset; everything else is ignored.
type dcrRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

// dcrResponse is the §3.2.1 success body. Subset of the spec, but
// includes everything Claude.ai's connector consumes.
type dcrResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
		return
	}
	var req dcrRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON: "+err.Error())
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	// Default response_types is ["code"], grant_types ["authorization_code"]
	// per RFC 7591 §2 — we mint refresh tokens too unless the client
	// opts out.
	if len(req.GrantTypes) == 0 {
		req.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	for _, gt := range req.GrantTypes {
		if gt != "authorization_code" && gt != "refresh_token" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported grant_type: "+gt)
			return
		}
	}
	if req.TokenEndpointAuthMethod == "" {
		req.TokenEndpointAuthMethod = "none" // public client, OAuth 2.1 default for PKCE
	}

	client, err := s.Auth.CreateOAuthClient(auth.CreateOAuthClientParams{
		ClientName:              req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              req.GrantTypes,
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
		Scope:                   req.Scope,
	})
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	}
	resp := dcrResponse{
		ClientID:                client.ClientID,
		ClientSecret:            client.ClientSecretPlaintext,
		ClientIDIssuedAt:        client.CreatedAt.Unix(),
		ClientName:              client.ClientName,
		RedirectURIs:            client.RedirectURIs,
		GrantTypes:              client.GrantTypes,
		TokenEndpointAuthMethod: client.TokenEndpointAuthMethod,
		Scope:                   client.Scope,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// ---------------- /oauth/authorize ----------------

// authorizePageData is the template payload for the consent page.
//
// SignedInUser is set when the request carried a valid session
// cookie — in that case the template renders the single-click
// "Logged in as @user — [Allow] [Deny]" branch. When unset, the
// template falls back to the username+password + paste-token form
// pair so a user with neither a session nor a PAT in their hand
// can still complete the flow.
type authorizePageData struct {
	ClientName          string
	ClientID            string
	RedirectURI         string
	State               string
	Scope               string
	Resource            string
	CodeChallenge       string
	CodeChallengeMethod string
	Error               string
	SignedInUser        string // empty when not session-authenticated
}

var authorizeTmpl = template.Must(template.New("authorize").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Authorize — AgentBoard</title>
<style>
:root { color-scheme: dark light; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
       background: #0b0c0f; color: #e8e8ea; margin: 0; min-height: 100vh;
       display: grid; place-items: center; }
.card { background: #16181d; border: 1px solid #2a2d36; border-radius: 12px;
        padding: 32px; width: 480px; max-width: calc(100vw - 32px); }
h1 { font-size: 18px; margin: 0 0 8px; }
p { color: #a8acb6; line-height: 1.5; margin: 8px 0; }
.client { background: #1e2128; padding: 14px 16px; border-radius: 8px; margin: 18px 0; }
.client b { color: #fff; }
.client small { color: #888c97; display: block; margin-top: 4px; word-break: break-all; }
label { display: block; font-size: 13px; color: #c5c8d2; margin: 18px 0 6px; }
input[type=password], input[type=text] {
  width: 100%; box-sizing: border-box; padding: 10px 12px; border-radius: 8px;
  border: 1px solid #2f323b; background: #0b0c0f; color: #fff;
  font-family: ui-monospace, SF Mono, Menlo, monospace; font-size: 13px;
}
.row { display: flex; gap: 10px; margin-top: 22px; }
button { flex: 1; padding: 11px 16px; border-radius: 8px; border: 0;
         font-size: 14px; font-weight: 600; cursor: pointer; }
.allow { background: #4a8eff; color: #fff; }
.deny  { background: #2a2d36; color: #c5c8d2; }
.error { background: #3a1f24; color: #f8b4b4; padding: 10px 12px; border-radius: 8px; margin-bottom: 14px; font-size: 13px; }
.signed-in { background: #1f2a1c; color: #b9e6a4; padding: 12px 14px; border-radius: 8px; margin: 18px 0; font-size: 14px; }
.divider   { color: #5a5e69; font-size: 12px; text-align: center; margin: 22px 0 6px; letter-spacing: 0.04em; }
</style>
</head>
<body>
<div class="card">
<h1>Authorize MCP access</h1>
<p>An MCP client wants to use your AgentBoard.</p>
<div class="client">
  <b>{{.ClientName}}</b>
  <small>{{.ClientID}}</small>
</div>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}

{{if .SignedInUser}}
<div class="signed-in">Signed in as <b>@{{.SignedInUser}}</b></div>
<form method="POST" action="/oauth/authorize/decide">
  <input type="hidden" name="client_id" value="{{.ClientID}}">
  <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
  <input type="hidden" name="state" value="{{.State}}">
  <input type="hidden" name="scope" value="{{.Scope}}">
  <input type="hidden" name="resource" value="{{.Resource}}">
  <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
  <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
  <input type="hidden" name="auth" value="session">
  <p><small style="color:#888c97">The client receives a separate, scoped credential — your session is not shared.</small></p>
  <div class="row">
    <button type="submit" name="decision" value="deny" class="deny">Deny</button>
    <button type="submit" name="decision" value="allow" class="allow" autofocus>Allow</button>
  </div>
</form>
{{else}}
<form method="POST" action="/oauth/authorize/decide">
  <input type="hidden" name="client_id" value="{{.ClientID}}">
  <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
  <input type="hidden" name="state" value="{{.State}}">
  <input type="hidden" name="scope" value="{{.Scope}}">
  <input type="hidden" name="resource" value="{{.Resource}}">
  <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
  <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
  <label for="username">Username</label>
  <input id="username" name="username" type="text" autocomplete="username" autofocus
         placeholder="alice">
  <label for="password">Password</label>
  <input id="password" name="password" type="password" autocomplete="current-password"
         placeholder="••••••••">
  <div class="divider">— or sign in with a token —</div>
  <label for="token">Token (<code>ab_...</code>)</label>
  <input id="token" name="token" type="password" autocomplete="off"
         placeholder="ab_...">
  <p><small style="color:#888c97">Your credentials authenticate this approval. They're not shared with the client; the client receives a separate, scoped credential.</small></p>
  <div class="row">
    <button type="submit" name="decision" value="deny" class="deny">Deny</button>
    <button type="submit" name="decision" value="allow" class="allow">Allow</button>
  </div>
</form>
{{end}}
</div>
</body>
</html>`))

func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	scope := strings.TrimSpace(q.Get("scope"))
	if scope == "" {
		scope = "mcp"
	}
	resource := q.Get("resource")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	responseType := q.Get("response_type")

	// Per OAuth 2.1: errors that *can* be returned via redirect (the
	// client and redirect_uri are validated) MUST go via redirect.
	// Errors that come from a bogus client_id / redirect_uri are
	// rendered directly to the user-agent so a malicious caller can't
	// bounce a victim to an attacker URL.
	client, err := s.Auth.GetOAuthClient(clientID)
	if err != nil || !slices.Contains(client.RedirectURIs, redirectURI) {
		http.Error(w, "unknown client_id or redirect_uri", http.StatusBadRequest)
		return
	}
	if responseType != "code" {
		redirectAuthorizeError(w, r, redirectURI, state, "unsupported_response_type", "only response_type=code is supported")
		return
	}
	if codeChallenge == "" || codeChallengeMethod != "S256" {
		redirectAuthorizeError(w, r, redirectURI, state, "invalid_request", "S256 code_challenge required")
		return
	}
	// Per RFC 8707 + MCP spec, clients MUST send `resource`. We default
	// to the canonical MCP URL when it's omitted (workaround for clients
	// that haven't caught up to the spec yet) but reject mismatches.
	canonical := auth.CanonicalMCPResourceURL(r)
	if resource == "" {
		resource = canonical
	}
	if resource != canonical {
		redirectAuthorizeError(w, r, redirectURI, state, "invalid_target", "resource must equal "+canonical)
		return
	}

	// If the user is already signed into this AgentBoard via the SPA
	// (session cookie present + valid), render the single-click
	// "Logged in as @user — Allow / Deny" branch. The decision
	// handler reads the same cookie and skips the password / token
	// prompts.
	signedIn := ""
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		if user, _, err := s.Auth.ResolveSession(cookie.Value); err == nil && user != nil {
			signedIn = user.Username
		}
	}

	_ = authorizeTmpl.Execute(w, authorizePageData{
		ClientName:          client.ClientName,
		ClientID:            client.ClientID,
		RedirectURI:         redirectURI,
		State:               state,
		Scope:               scope,
		Resource:            resource,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		SignedInUser:        signedIn,
	})
}

func (s *Server) handleOAuthAuthorizeDecide(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")
	scope := r.FormValue("scope")
	resource := r.FormValue("resource")
	codeChallenge := r.FormValue("code_challenge")
	codeChallengeMethod := r.FormValue("code_challenge_method")
	decision := r.FormValue("decision")
	tokenStr := strings.TrimSpace(r.FormValue("token"))
	usernameForm := strings.TrimSpace(r.FormValue("username"))
	passwordForm := r.FormValue("password")

	client, err := s.Auth.GetOAuthClient(clientID)
	if err != nil || !slices.Contains(client.RedirectURIs, redirectURI) {
		http.Error(w, "unknown client_id or redirect_uri", http.StatusBadRequest)
		return
	}

	if decision != "allow" {
		redirectAuthorizeError(w, r, redirectURI, state, "access_denied", "user denied authorization")
		return
	}

	// Re-validate authorize-time invariants so a tampered form can't
	// downgrade challenge method / swap audience between page render
	// and submission.
	if codeChallenge == "" || codeChallengeMethod != "S256" {
		redirectAuthorizeError(w, r, redirectURI, state, "invalid_request", "PKCE S256 required")
		return
	}
	canonical := auth.CanonicalMCPResourceURL(r)
	if resource == "" {
		resource = canonical
	}
	if resource != canonical {
		redirectAuthorizeError(w, r, redirectURI, state, "invalid_target", "resource must equal "+canonical)
		return
	}

	// Three auth signals, tried in priority order:
	//   1. session cookie (the SPA-logged-in user — the
	//      one-click "Logged in as @user" branch),
	//   2. username + password (the new browser-friendly
	//      sign-in path),
	//   3. pasted PAT (the legacy paste-a-token path; kept
	//      for users who only have a token in their hand).
	//
	// Whichever resolves the user first wins. The user-facing
	// error never distinguishes which signal failed — same shape
	// regardless, so a hostile observer can't tell from the
	// rendered page whether @alice exists.
	var user *auth.User
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		if u, _, err := s.Auth.ResolveSession(cookie.Value); err == nil {
			user = u
		}
	}
	if user == nil && usernameForm != "" && passwordForm != "" {
		if u, err := s.Auth.VerifyLogin(usernameForm, passwordForm); err == nil {
			user = u
		}
	}
	if user == nil && tokenStr != "" {
		if u, _, err := s.Auth.ResolveToken(auth.HashToken(tokenStr)); err == nil {
			user = u
		}
	}
	if user == nil {
		// Re-render the page with an error rather than redirecting —
		// this is an authentication failure, not an authorization
		// decision. Sending an error back to the redirect_uri would
		// leak that the user mistyped.
		_ = authorizeTmpl.Execute(w, authorizePageData{
			ClientName:          client.ClientName,
			ClientID:            client.ClientID,
			RedirectURI:         redirectURI,
			State:               state,
			Scope:               scope,
			Resource:            resource,
			CodeChallenge:       codeChallenge,
			CodeChallengeMethod: codeChallengeMethod,
			Error:               "Sign-in failed. Check your username + password, or paste a valid token.",
		})
		return
	}

	code, err := s.Auth.CreateAuthCode(client.ClientID, user.Username, redirectURI,
		codeChallenge, codeChallengeMethod, scope, resource)
	if err != nil {
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	target, _ := url.Parse(redirectURI)
	q := target.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func redirectAuthorizeError(w http.ResponseWriter, r *http.Request, redirectURI, state, code, desc string) {
	target, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := target.Query()
	q.Set("error", code)
	if desc != "" {
		q.Set("error_description", desc)
	}
	if state != "" {
		q.Set("state", state)
	}
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// ---------------- /oauth/token ----------------

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}
	grantType := r.FormValue("grant_type")

	// Client authentication. For confidential clients accept both
	// HTTP Basic and form-body credentials per OAuth 2.1 §2.3.1.
	clientID, clientSecret := extractClientCredentials(r)
	if clientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "client_id required")
		return
	}
	client, err := s.Auth.GetOAuthClient(clientID)
	if err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		return
	}
	if client.HasSecret {
		if clientSecret == "" || !s.Auth.VerifyClientSecret(clientID, clientSecret) {
			w.Header().Set("WWW-Authenticate", `Basic realm="oauth"`)
			writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
			return
		}
	}

	switch grantType {
	case "authorization_code":
		s.handleAuthorizationCodeGrant(w, r, client)
	case "refresh_token":
		s.handleRefreshTokenGrant(w, r, client)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type: "+grantType)
	}
}

func (s *Server) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request, client *auth.OAuthClient) {
	code := r.FormValue("code")
	verifier := r.FormValue("code_verifier")
	redirectURI := r.FormValue("redirect_uri")
	resource := r.FormValue("resource")

	if code == "" || verifier == "" || redirectURI == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code, code_verifier, redirect_uri required")
		return
	}
	rec, err := s.Auth.ConsumeAuthCode(code)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code invalid or expired")
		return
	}
	if rec.ClientID != client.ClientID || rec.RedirectURI != redirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code does not match client / redirect_uri")
		return
	}
	if !auth.VerifyPKCEChallenge(verifier, rec.CodeChallenge, rec.CodeChallengeMethod) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}
	// If the client sends `resource` at the token endpoint, it MUST
	// match the audience the code was issued for.
	if resource != "" && resource != rec.Audience {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource mismatch")
		return
	}

	withRefresh := slices.Contains(client.GrantTypes, "refresh_token")
	issued, err := s.Auth.CreateAccessToken(auth.CreateAccessTokenParams{
		ClientID:    client.ClientID,
		Username:    rec.Username,
		Scope:       rec.Scope,
		Audience:    rec.Audience,
		WithRefresh: withRefresh,
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	writeTokenResponse(w, issued)
}

func (s *Server) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request, client *auth.OAuthClient) {
	refresh := r.FormValue("refresh_token")
	if refresh == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "refresh_token required")
		return
	}
	issued, err := s.Auth.RotateRefreshToken(refresh, client.ClientID)
	if err != nil {
		if errors.Is(err, auth.ErrOAuthTokenInvalid) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token invalid or expired")
			return
		}
		writeOAuthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	writeTokenResponse(w, issued)
}

func writeTokenResponse(w http.ResponseWriter, issued *auth.AccessTokenIssued) {
	body := map[string]any{
		"access_token": issued.AccessToken,
		"token_type":   "Bearer",
		"expires_in":   issued.AccessExpiresIn,
		"scope":        issued.Scope,
	}
	if issued.RefreshToken != "" {
		body["refresh_token"] = issued.RefreshToken
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(body)
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": desc,
	})
}

// extractClientCredentials reads (client_id, client_secret) from
// HTTP Basic OR form parameters per OAuth 2.1 §2.3.1.
func extractClientCredentials(r *http.Request) (string, string) {
	if user, pass, ok := r.BasicAuth(); ok && user != "" {
		return user, pass
	}
	return r.FormValue("client_id"), r.FormValue("client_secret")
}
