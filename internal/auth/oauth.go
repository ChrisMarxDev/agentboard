package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// CanonicalBaseURL derives the public origin (scheme://host) of the
// AgentBoard server from an incoming request, respecting Cloudflare /
// reverse-proxy headers. Used to build self-referential URLs in OAuth
// discovery documents (RFC 8414, RFC 9728) and the WWW-Authenticate
// resource_metadata pointer.
//
// Production runs behind Cloudflare which terminates TLS and forwards
// X-Forwarded-Proto/Host; localhost dev hits the Go server directly.
// The forwarded headers, when present, take precedence.
func CanonicalBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := firstHop(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	}
	host := r.Host
	if h := firstHop(r.Header.Get("X-Forwarded-Host")); h != "" {
		host = h
	}
	return scheme + "://" + host
}

// CanonicalMCPResourceURL returns the OAuth resource identifier that
// MCP clients MUST send as the `resource` parameter and that this server
// MUST validate as the audience claim of incoming access tokens.
// Defined as base + "/mcp" per RFC 8707 §2 with no trailing slash.
func CanonicalMCPResourceURL(r *http.Request) string {
	return CanonicalBaseURL(r) + "/mcp"
}

// resourceMetadataURL is where this server exposes its RFC 9728
// Protected Resource Metadata document. Embedded in the
// WWW-Authenticate Bearer challenge so unauthenticated MCP clients can
// discover the authorization server.
func resourceMetadataURL(r *http.Request) string {
	return CanonicalBaseURL(r) + "/.well-known/oauth-protected-resource"
}

// BearerChallenge returns the WWW-Authenticate header value MCP clients
// expect on a 401 response. Format per RFC 9728 §5.1.
func BearerChallenge(r *http.Request) string {
	return `Bearer realm="AgentBoard", resource_metadata="` + resourceMetadataURL(r) + `"`
}

// HandleProtectedResourceMetadata serves RFC 9728 metadata describing
// the MCP endpoint as a protected resource and pointing at the
// authorization server clients should use to obtain tokens.
func HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := CanonicalBaseURL(r)
	writeDiscoveryJSON(w, map[string]any{
		"resource":                 base + "/mcp",
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{"mcp"},
		"resource_name":            "AgentBoard MCP",
		"resource_documentation":   base + "/api/introduction",
	})
}

// HandleAuthorizationServerMetadata serves RFC 8414 metadata describing
// the OAuth 2.1 authorization server hosted in the same binary. The
// endpoints advertised here are implemented in /oauth/* handlers.
func HandleAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := CanonicalBaseURL(r)
	writeDiscoveryJSON(w, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic", "client_secret_post"},
		"scopes_supported":                      []string{"mcp"},
	})
}

func writeDiscoveryJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(body)
}

// firstHop returns the leftmost value in a comma-separated header
// (proxy chains can append; the first hop is the original).
func firstHop(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if i := strings.IndexByte(v, ','); i >= 0 {
		return strings.TrimSpace(v[:i])
	}
	return v
}
