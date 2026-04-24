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

// UserFromContext returns the User attached to the request, or nil.
func UserFromContext(ctx context.Context) *User {
	v, _ := ctx.Value(ctxUser).(*User)
	return v
}

// TokenFromContext returns the specific token row used to authenticate
// the request, or nil.
func TokenFromContext(ctx context.Context) *UserToken {
	v, _ := ctx.Value(ctxToken).(*UserToken)
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
// There is no "zero-users = open" shortcut — fresh instances route
// through /api/setup to claim admin. The server never runs unauthed
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
				http.Error(w, "forbidden", http.StatusForbidden)
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
	// Deliberately no WWW-Authenticate header. That header triggers the
	// browser's native Basic Auth popup; we don't want it because the
	// SPA has its own /login page and catches 401s via apiFetch.
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
