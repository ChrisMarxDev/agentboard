package auth

import (
	"context"
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
)

// UserFromContext returns the User attached to the request, or nil if auth
// didn't run (open mode) or no middleware attached one.
func UserFromContext(ctx context.Context) *User {
	v, _ := ctx.Value(ctxUser).(*User)
	return v
}

// TokenFromContext returns the specific token row used to authenticate the
// request, or nil. Useful when we want to log which token performed a
// write (a step down from just knowing the user).
func TokenFromContext(ctx context.Context) *UserToken {
	v, _ := ctx.Value(ctxToken).(*UserToken)
	return v
}

// MiddlewareConfig tunes middleware behavior. Zero values are safe defaults.
type MiddlewareConfig struct {
	// OpenPaths are HTTP paths that skip all auth. /api/health is always
	// open regardless.
	OpenPaths []string
}

// TokenMiddleware resolves the Bearer / Basic / ?token= credential to a
// user and attaches both to the request context.
//
//  1. /api/health and OPTIONS preflights pass through.
//  2. If no users exist yet (fresh install pre-bootstrap) the request
//     passes through to preserve the legacy "loopback open" posture.
//  3. Otherwise resolve the token. 401 on missing/unknown/revoked;
//     403-ish falls to the admin gate if needed.
//
// last_used_at on the token row is bumped at most once per minute per
// token so this stays off the hot path.
func TokenMiddleware(store *Store, cfg MiddlewareConfig) func(http.Handler) http.Handler {
	openSet := buildOpenSet(cfg.OpenPaths)
	updater := newUsageUpdater(store)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if passThrough(r, openSet) {
				next.ServeHTTP(w, r)
				return
			}

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
			user, tok, err := store.ResolveToken(HashToken(token))
			if err != nil {
				if errors.Is(err, ErrNotFound) || errors.Is(err, ErrTokenRevoked) || errors.Is(err, ErrUserDeactivated) {
					unauthorized(w)
					return
				}
				http.Error(w, "auth lookup error", http.StatusInternalServerError)
				return
			}
			updater.touch(tok.ID)

			ctx := context.WithValue(r.Context(), ctxUser, user)
			ctx = context.WithValue(ctx, ctxToken, tok)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthorizeMiddleware enforces per-user access_mode + rules. Reads the
// user from context — only call after TokenMiddleware.
//
// Admin users are exempt — admin = full trust, rules don't apply. This
// avoids the "admin wrote a bad rule and locked themselves out" footgun.
func AuthorizeMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil || user.Kind == KindAdmin {
				next.ServeHTTP(w, r)
				return
			}
			if !Authorize(user.AccessMode, user.Rules, r.Method, r.URL.Path) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AdminRequired rejects requests whose user isn't an admin. Scope around
// /api/admin/*.
func AdminRequired() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			user := UserFromContext(r.Context())
			if user == nil || user.Kind != KindAdmin {
				http.Error(w, "admin token required", http.StatusForbidden)
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

func unauthorized(w http.ResponseWriter) {
	// Deliberately no WWW-Authenticate header. That header is what triggers
	// the browser's native Basic Auth popup; we don't want it because the
	// SPA has its own /login page. Plain 401 lets fetch() see the status
	// without the browser intercepting, and lets the apiFetch wrapper
	// redirect to /login on its own terms.
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

// first-run cache so the users-count check isn't a per-request DB hit.
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

	has, err := store.HasAnyUser()
	if err != nil {
		return false, err
	}
	isFirst := !has

	firstRunCacheMu.Lock()
	firstRunCacheValue = isFirst
	firstRunCacheSet = true
	firstRunCacheExpiry = time.Now().Add(1 * time.Second)
	firstRunCacheMu.Unlock()
	return isFirst, nil
}

func InvalidateFirstRunCache() {
	firstRunCacheMu.Lock()
	firstRunCacheSet = false
	firstRunCacheMu.Unlock()
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
	u.mu.Lock()
	last, ok := u.seen[tokenID]
	if ok && time.Since(last) < time.Minute {
		u.mu.Unlock()
		return
	}
	u.seen[tokenID] = time.Now()
	u.mu.Unlock()
	_ = u.store.TouchTokenLastUsed(tokenID)
}
