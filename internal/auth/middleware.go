package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Session cookie name. Must match the frontend.
const SessionCookieName = "ab_session"

// Session TTL — absolute. Idle timeout is handled separately by checking
// LastSeenAt against a sliding window.
const SessionTTL = 7 * 24 * time.Hour

// IdleTimeout forces re-login after this much inactivity, independent of
// the absolute TTL.
const IdleTimeout = 2 * time.Hour

// ctxKey is the type used for context values so our keys can't collide
// with others.
type ctxKey int

const (
	ctxIdentity ctxKey = iota + 1
	ctxSession
)

// IdentityFromContext returns the Identity attached to the request's context,
// or nil if no auth middleware ran (open mode) or auth failed.
func IdentityFromContext(ctx context.Context) *Identity {
	v, _ := ctx.Value(ctxIdentity).(*Identity)
	return v
}

// SessionFromContext returns the admin Session attached to the request's
// context, or nil if the request wasn't admin-session-authenticated.
func SessionFromContext(ctx context.Context) *Session {
	v, _ := ctx.Value(ctxSession).(*Session)
	return v
}

// MiddlewareConfig tunes how the middlewares behave. Zero values are safe
// defaults.
type MiddlewareConfig struct {
	// OpenPaths are HTTP paths that skip all auth. /api/health is always
	// open regardless; OpenPaths extends that list (currently used for
	// /api/admin/setup and /api/admin/login).
	OpenPaths []string
}

// TokenMiddleware gates every non-open data-plane route by an agent token.
// Flow:
//
//  1. If /api/health or an OPTIONS preflight → pass through.
//  2. If no identities have ever been created AND no admin has ever been
//     created → run in open mode. This preserves the "local-only, no auth"
//     posture of `task run` and first-run.
//  3. Otherwise resolve the presented token (Bearer, Basic password, or
//     ?token=) to an identity. Missing/invalid → 401. Revoked → 401.
//  4. Attach the identity to the request context and call the next handler.
//     (Rule authorization happens in AuthorizeMiddleware below, which can be
//     scoped to specific subtrees.)
//
// last_used_at is bumped asynchronously so it doesn't slow the hot path.
func TokenMiddleware(store *Store, cfg MiddlewareConfig) func(http.Handler) http.Handler {
	openSet := buildOpenSet(cfg.OpenPaths)
	updater := newUsageUpdater(store)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if passThrough(r, openSet) {
				next.ServeHTTP(w, r)
				return
			}

			// Admin endpoints are handled by SessionMiddleware; the token
			// realm never reaches them.
			if strings.HasPrefix(r.URL.Path, "/api/admin/") {
				next.ServeHTTP(w, r)
				return
			}

			// Open-mode shortcut: if there are literally no identities in
			// the DB, the server runs open like before auth-v2 landed.
			open, err := isFirstRun(store)
			if err != nil {
				http.Error(w, "auth backend error", http.StatusInternalServerError)
				return
			}
			if open {
				next.ServeHTTP(w, r)
				return
			}

			token := extractToken(r)
			if token == "" {
				unauthorized(w)
				return
			}

			ident, err := store.GetIdentityByTokenHash(HashToken(token))
			if err != nil {
				if errors.Is(err, ErrNotFound) || errors.Is(err, ErrRevoked) {
					unauthorized(w)
					return
				}
				http.Error(w, "auth lookup error", http.StatusInternalServerError)
				return
			}

			// Only agents authenticate via tokens on the data plane. An admin
			// row with a token_hash is unusual but not impossible; treat it
			// like an agent for data-plane purposes (admin-plane still
			// requires a cookie session).
			updater.touch(ident.ID)

			ctx := context.WithValue(r.Context(), ctxIdentity, ident)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthorizeMiddleware enforces the identity's access_mode + rules. Wrap it
// around the subtree you want to gate (typically /api/*). It reads the
// identity from context — only call after TokenMiddleware.
//
// When no identity is attached (open-mode request), the middleware allows
// the request: the gating already happened upstream.
func AuthorizeMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ident := IdentityFromContext(r.Context())
			if ident == nil {
				next.ServeHTTP(w, r)
				return
			}
			if !Authorize(ident.AccessMode, ident.Rules, r.Method, r.URL.Path) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SessionMiddleware gates /api/admin/* by a valid admin session cookie.
// Open paths (setup, login) bypass the check — they're expected to run
// before a session exists. Everything else 401s without a valid cookie.
//
// On success the session + identity are attached to context; handlers
// pull them via SessionFromContext / IdentityFromContext.
//
// CSRF: state-changing methods (POST/PUT/PATCH/DELETE) also require an
// X-CSRF-Token header matching the session's stored csrf token. The
// token is exposed via GET /api/admin/me and is safe to stash in a
// page-level JS variable because SameSite=Strict keeps the cookie
// browser-origin only.
func SessionMiddleware(store *Store, cfg MiddlewareConfig) func(http.Handler) http.Handler {
	openSet := buildOpenSet(cfg.OpenPaths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := openSet[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie(SessionCookieName)
			if err != nil || cookie.Value == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			sess, err := store.GetSession(cookie.Value)
			if err != nil {
				if errors.Is(err, ErrNotFound) || errors.Is(err, ErrSessionExpired) {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				http.Error(w, "auth backend error", http.StatusInternalServerError)
				return
			}

			// Idle timeout.
			if time.Since(sess.LastSeenAt) > IdleTimeout {
				_ = store.DeleteSession(sess.ID)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ident, err := store.GetIdentity(sess.IdentityID)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if ident.RevokedAt != nil || ident.Kind != KindAdmin {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// CSRF check on state-changing methods.
			if isStateChanging(r.Method) {
				presented := r.Header.Get("X-CSRF-Token")
				if presented == "" || presented != sess.CSRFToken {
					http.Error(w, "csrf token mismatch", http.StatusForbidden)
					return
				}
			}

			// Refresh last_seen_at inline; it's a single indexed UPDATE.
			_ = store.TouchSession(sess.ID)

			ctx := context.WithValue(r.Context(), ctxSession, sess)
			ctx = context.WithValue(ctx, ctxIdentity, ident)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ------------------------------------------------------------------
// helpers
// ------------------------------------------------------------------

// extractToken pulls a token from Authorization (Bearer), HTTP Basic password,
// or ?token= query parameter — same shape as the legacy authMiddleware.
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

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="agentboard"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
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

func isStateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// isFirstRun reports whether the server has zero identities — in which case
// the middleware runs open. Cached for 1s to avoid a DB hit per request at
// steady state; the cache expires quickly so the moment the first identity
// is created, the next request (within a second) picks it up.
var (
	firstRunCacheMu     sync.Mutex
	firstRunCacheValue  bool
	firstRunCacheSet    bool
	firstRunCacheExpiry time.Time
)

func isFirstRun(store *Store) (bool, error) {
	firstRunCacheMu.Lock()
	if firstRunCacheSet && time.Now().Before(firstRunCacheExpiry) {
		v := firstRunCacheValue
		firstRunCacheMu.Unlock()
		return v, nil
	}
	firstRunCacheMu.Unlock()

	idents, err := store.ListIdentities()
	if err != nil {
		return false, err
	}
	isFirst := len(idents) == 0

	firstRunCacheMu.Lock()
	firstRunCacheValue = isFirst
	firstRunCacheSet = true
	firstRunCacheExpiry = time.Now().Add(1 * time.Second)
	firstRunCacheMu.Unlock()
	return isFirst, nil
}

// InvalidateFirstRunCache lets handlers that just created the first identity
// force the next request to re-check, avoiding a one-second window where
// the open-mode shortcut is still true.
func InvalidateFirstRunCache() {
	firstRunCacheMu.Lock()
	firstRunCacheSet = false
	firstRunCacheMu.Unlock()
}

// usageUpdater coalesces last_used_at bumps so we don't issue a DB write
// on every request for a hot token. At most one write per identity per
// minute.
type usageUpdater struct {
	store *Store
	mu    sync.Mutex
	seen  map[string]time.Time
}

func newUsageUpdater(store *Store) *usageUpdater {
	return &usageUpdater{store: store, seen: make(map[string]time.Time)}
}

func (u *usageUpdater) touch(id string) {
	u.mu.Lock()
	last, ok := u.seen[id]
	if ok && time.Since(last) < time.Minute {
		u.mu.Unlock()
		return
	}
	u.seen[id] = time.Now()
	u.mu.Unlock()
	// Coalescing to 1/minute keeps this off the hot path without a goroutine —
	// async writes against SQLite can collide with reads under test-suite load.
	_ = u.store.TouchLastUsed(id)
}
