package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ctxKey is the type used for context values so our keys can't collide.
type ctxKey int

const (
	ctxUser ctxKey = iota + 1
	ctxToken
	ctxOAuth
)

// UserFromContext returns the User attached to the request, or nil.
func UserFromContext(ctx context.Context) *User {
	v, _ := ctx.Value(ctxUser).(*User)
	return v
}

// TokenFromContext returns the specific PAT token row used to
// authenticate the request, or nil. Returns nil when the request was
// authenticated via an OAuth-issued access token (see OAuthFromContext).
func TokenFromContext(ctx context.Context) *UserToken {
	v, _ := ctx.Value(ctxToken).(*UserToken)
	return v
}

// OAuthFromContext returns the OAuth access record used to authenticate
// the request, or nil. Populated only when the Bearer carried `oat_*`.
func OAuthFromContext(ctx context.Context) *OAuthAccessRecord {
	v, _ := ctx.Value(ctxOAuth).(*OAuthAccessRecord)
	return v
}

// MiddlewareConfig tunes middleware behavior. Zero values are safe defaults.
type MiddlewareConfig struct {
	// OpenPaths are HTTP paths that skip all auth. /api/health is always
	// open regardless. Used today for the /api/setup/* flow, which is
	// only reachable before a user exists.
	OpenPaths []string
}

// TokenMiddleware resolves the Bearer / Basic / ?token= credential to a
// (user, token) pair and attaches both to the request context.
//
//  1. /api/health and OPTIONS preflights pass through.
//  2. Configured OpenPaths pass through.
//  3. Otherwise a valid token is required. Missing/revoked/deactivated
//     → 401. The SPA catches 401 via apiFetch and redirects to /login.
//
// Two token spaces are accepted: PATs (`ab_*`, the existing user_tokens
// surface) and OAuth-issued access tokens (`oat_*`, minted via the
// /oauth/* flow for Claude.ai-style Custom Connectors). OAuth tokens
// are audience-bound — they're only valid against the MCP resource
// they were issued for, enforced here.
//
// There is no "zero-users = open" shortcut — fresh instances route
// through /invite/* to claim admin. The server never runs unauthed
// except on explicitly open paths.
//
// last_used_at on the token row is bumped at most once per minute per
// token so it stays off the hot path.
func TokenMiddleware(store *Store, cfg MiddlewareConfig) func(http.Handler) http.Handler {
	openSet := buildOpenSet(cfg.OpenPaths)
	updater := newUsageUpdater(store)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if passThrough(r, openSet) {
				next.ServeHTTP(w, r)
				return
			}
			token := extractToken(r)
			if token == "" {
				unauthorized(w, r)
				return
			}
			hash := HashToken(token)

			// OAuth-issued access tokens carry an explicit prefix so we
			// can route them to the audience-aware resolver without a
			// double DB hit on bogus credentials.
			if strings.HasPrefix(token, OAuthAccessPrefix) {
				rec, user, err := store.ResolveAccessToken(hash)
				if err != nil {
					if errors.Is(err, ErrOAuthTokenInvalid) || errors.Is(err, ErrUserDeactivated) {
						unauthorized(w, r)
						return
					}
					writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "auth lookup error")
					return
				}
				// Per the MCP authorization spec (RFC 8707 + 9728), an
				// OAuth access token is bound to a single MCP resource
				// (its audience). Refuse to validate it for any other
				// path or any other host this binary serves.
				if !isMCPPath(r.URL.Path) || rec.Audience != CanonicalMCPResourceURL(r) {
					unauthorized(w, r)
					return
				}
				updater.touchOAuth(rec.ID)
				ctx := context.WithValue(r.Context(), ctxUser, user)
				ctx = context.WithValue(ctx, ctxOAuth, rec)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// PAT path — unchanged.
			user, tok, err := store.ResolveToken(hash)
			if err != nil {
				if errors.Is(err, ErrNotFound) || errors.Is(err, ErrTokenRevoked) || errors.Is(err, ErrUserDeactivated) {
					unauthorized(w, r)
					return
				}
				writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "auth lookup error")
				return
			}
			updater.touch(tok.ID)

			ctx := context.WithValue(r.Context(), ctxUser, user)
			ctx = context.WithValue(ctx, ctxToken, tok)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isMCPPath reports whether the request is targeting the MCP resource.
// Kept narrow on purpose: OAuth tokens are bound to /mcp (canonical
// resource) and must not unlock unrelated /api/* surfaces.
func isMCPPath(p string) bool {
	return p == "/mcp" || strings.HasPrefix(p, "/mcp/")
}

// AuthorizeMiddleware enforces per-user access_mode + rules. Reads the
// user from context — only call after TokenMiddleware.
//
// Admin users bypass rule evaluation: admin = full trust, rules don't
// apply. Avoids the "admin wrote a bad rule and locked themselves out"
// footgun.
func AuthorizeMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil || user.Kind == KindAdmin {
				next.ServeHTTP(w, r)
				return
			}
			if !Authorize(user.AccessMode, user.Rules, r.Method, r.URL.Path) {
				writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "access denied by per-user rule")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AdminRequired rejects requests whose user isn't admin-kind. Scope
// around /api/admin/*.
func AdminRequired() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			user := UserFromContext(r.Context())
			if user == nil || user.Kind != KindAdmin {
				writeJSONError(w, http.StatusForbidden, "ADMIN_REQUIRED", "admin token required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ScopeSelfOrAdmin enforces the "self OR admin" rule for per-user
// token-management endpoints. Expects a chi URL param named `username`
// to have been bound by an outer router; reads it via urlParamReader
// so this package doesn't depend on chi directly.
//
// Rule:
//   - kind(caller) == admin → allowed for any target.
//   - target == caller.username → allowed (users own themselves).
//   - Otherwise 403.
//
// The bot-vs-member nuance from the roadmap ("admin manages bot
// tokens") is covered by rule 1; members and bots alike fail rule 2
// when targeting another user.
func ScopeSelfOrAdmin(readParam func(r *http.Request, name string) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			caller := UserFromContext(r.Context())
			if caller == nil {
				writeJSONError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
				return
			}
			if caller.Kind == KindAdmin {
				next.ServeHTTP(w, r)
				return
			}
			target := strings.ToLower(readParam(r, "username"))
			if target == "" || target != strings.ToLower(caller.Username) {
				writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "can only manage own tokens")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ------------------------------------------------------------------
// helpers
// ------------------------------------------------------------------

func extractToken(r *http.Request) string {
	if ah := r.Header.Get("Authorization"); ah != "" {
		if rest, ok := strings.CutPrefix(ah, "Bearer "); ok && rest != "" {
			return rest
		}
		if _, pw, ok := r.BasicAuth(); ok && pw != "" {
			return pw
		}
	}
	return r.URL.Query().Get("token")
}

func unauthorized(w http.ResponseWriter, r *http.Request) {
	// Always emit a Bearer challenge with the protected-resource-metadata
	// URL so OAuth-aware MCP clients (Claude.ai Custom Connectors, any
	// client following the MCP authorization spec) can discover the
	// authorization server from a bare 401. RFC 9728 §5.1.
	w.Header().Add("WWW-Authenticate", BearerChallenge(r))

	// Browser top-level navigations also get a Basic challenge so the
	// native auth prompt fires — paste the token as the password and
	// the request retries via r.BasicAuth() → token. Programmatic /
	// fetch() callers (Sec-Fetch-Mode != "navigate") skip this; the SPA
	// handles 401 via its /login redirect in apiFetch.
	if r.Header.Get("Sec-Fetch-Mode") == "navigate" {
		w.Header().Add("WWW-Authenticate", `Basic realm="AgentBoard"`)
	}
	writeJSONError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
}

// writeJSONError mirrors server.respondError for the auth package. Kept
// inline to avoid an import cycle (server depends on auth, not the
// other way around). Every error path through the auth middleware
// emits this shape so agents can JSON-parse error bodies uniformly.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "error": message})
}

func passThrough(r *http.Request, openSet map[string]struct{}) bool {
	if r.Method == http.MethodOptions {
		return true
	}
	if r.URL.Path == "/api/health" {
		return true
	}
	if _, ok := openSet[r.URL.Path]; ok {
		return true
	}
	return false
}

func buildOpenSet(paths []string) map[string]struct{} {
	m := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		m[p] = struct{}{}
	}
	return m
}

// usageUpdater coalesces last_used_at bumps to one write per minute per
// token so the hot path stays allocation-only.
type usageUpdater struct {
	store *Store
	mu    sync.Mutex
	seen  map[string]time.Time
}

func newUsageUpdater(store *Store) *usageUpdater {
	return &usageUpdater{store: store, seen: make(map[string]time.Time)}
}

func (u *usageUpdater) touch(tokenID string) {
	if u.shouldUpdate(tokenID) {
		_ = u.store.TouchTokenLastUsed(tokenID)
	}
}

func (u *usageUpdater) touchOAuth(tokenID string) {
	if u.shouldUpdate("oauth:" + tokenID) {
		_ = u.store.TouchOAuthLastUsed(tokenID)
	}
}

// shouldUpdate is the coalescing gate: returns true at most once per
// minute for any given key, so the hot path stays allocation-only.
func (u *usageUpdater) shouldUpdate(key string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	last, ok := u.seen[key]
	if ok && time.Since(last) < time.Minute {
		return false
	}
	u.seen[key] = time.Now()
	return true
}
