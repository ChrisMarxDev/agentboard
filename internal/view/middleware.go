package view

import (
	"context"
	"errors"
	"net/http"

	"github.com/christophermarx/agentboard/internal/auth"
)

// ctxKey scopes our context keys.
type ctxKey int

const (
	ctxScope ctxKey = iota + 1
	ctxSession
)

// ScopeFromContext returns the resolved scope attached by the view
// middleware, or nil if no scope has been attached (error path).
func ScopeFromContext(ctx context.Context) *Scope {
	v, _ := ctx.Value(ctxScope).(*Scope)
	return v
}

// SessionFromContext returns the cookie-backed share session attached
// by the middleware, or nil.
func SessionFromContext(ctx context.Context) *Session {
	v, _ := ctx.Value(ctxSession).(*Session)
	return v
}

// WithScope attaches a scope for downstream handlers (tests mostly).
func WithScope(ctx context.Context, s *Scope) context.Context {
	return context.WithValue(ctx, ctxScope, s)
}

// WithSession attaches a session for downstream handlers (tests mostly).
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, ctxSession, s)
}

// ResolveAuthority inspects an incoming request and returns the
// authority kind + user. Bearer → Admin/Agent. Cookie → Share. Neither
// → Anonymous. Does NOT decide whether the request is actually
// authorised — that's Scope's job.
//
// Resolves the bearer directly against the auth store (no middleware
// required) so the broker can mount outside the gated group. Prevents
// the situation where a share-cookie visitor gets 401'd by the
// authentication layer before the broker sees them.
//
// Error semantics: a non-nil error signals a *transient* failure
// (typically a SQLite hiccup during an in-flight write). Callers
// should map that to 503, not 401. Without this, a freshly-redeemed
// admin can hit a brief contention window where ResolveToken fails,
// fall through to AuthorityAnonymous, then 401, then bounce to
// /login — clearing a perfectly good token along the way. ErrNotFound
// / revoked / deactivated do NOT count as transient: they bubble up
// as Anonymous so the public-paths gate decides.
//
// Shares: the resolved session's path overrides the request's view
// path. A share-cookie visitor hitting /api/view/open?path=other gets
// re-anchored to their share's path. Prevents using a share cookie to
// probe other views.
func ResolveAuthority(r *http.Request, authStore *auth.Store, sessions *SessionStore) (AuthorityKind, *auth.User, *Session, error) {
	if authStore != nil {
		if token := extractBearerToken(r); token != "" {
			user, _, err := authStore.ResolveToken(auth.HashToken(token))
			if err == nil && user != nil {
				if user.Kind == auth.KindAdmin {
					return AuthorityAdmin, user, nil, nil
				}
				return AuthorityAgent, user, nil, nil
			}
			// ErrNotFound / revoked / deactivated → fall through to
			// the cookie/anonymous branches. Anything else is
			// transient and must surface as a server error.
			if err != nil &&
				!errors.Is(err, auth.ErrNotFound) &&
				!errors.Is(err, auth.ErrTokenRevoked) &&
				!errors.Is(err, auth.ErrUserDeactivated) {
				return AuthorityAnonymous, nil, nil, err
			}
		}
	}
	if sessions != nil {
		if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
			if sess, err := sessions.Resolve(c.Value); err == nil {
				return AuthorityShare, nil, sess, nil
			}
		}
	}
	return AuthorityAnonymous, nil, nil, nil
}

func extractBearerToken(r *http.Request) string {
	if ah := r.Header.Get("Authorization"); ah != "" {
		const prefix = "Bearer "
		if len(ah) > len(prefix) && ah[:len(prefix)] == prefix {
			return ah[len(prefix):]
		}
		if _, pw, ok := r.BasicAuth(); ok && pw != "" {
			return pw
		}
	}
	return r.URL.Query().Get("token")
}
