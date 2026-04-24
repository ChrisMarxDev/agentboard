package publicroutes

import (
	"context"
	"net/http"
	"strings"
)

// ctxKey is the private type for context values so our keys can't
// collide with other packages'.
type ctxKey int

const ctxPublic ctxKey = 1

// IsPublicRequest reports whether the request arrived via a public
// path and therefore bypassed auth. Handlers can use this to adjust
// responses — e.g. hide draft content, omit internal fields.
func IsPublicRequest(ctx context.Context) bool {
	v, _ := ctx.Value(ctxPublic).(bool)
	return v
}

// GateOptions configures the public-route gate.
type GateOptions struct {
	// AdminPathPrefix is excluded from public matching unconditionally —
	// /api/admin/* must always require auth no matter what an operator
	// wrote in their config. Defaults to "/api/admin".
	AdminPathPrefix string
}

// Gate wraps an inner auth middleware. When the request matches a
// public read rule, Gate calls next directly (skipping auth). Otherwise
// it delegates to the wrapped auth middleware.
//
// This is the ONLY place in the request path where auth can be bypassed
// by configuration. /api/health and method=OPTIONS are already handled
// inside the auth middleware's passThrough; Gate adds the glob-configured
// public-read pathway on top.
//
// Invariant: only GET/HEAD/OPTIONS can ever be public. Writes delegate
// to auth unconditionally, enforced by Matcher.IsPubliclyReadable.
func Gate(m *Matcher, authMiddleware func(http.Handler) http.Handler, opts GateOptions) func(http.Handler) http.Handler {
	adminPrefix := opts.AdminPathPrefix
	if adminPrefix == "" {
		adminPrefix = "/api/admin"
	}
	// If no patterns are configured, Gate is a no-op wrapper.
	if m == nil || !m.HasRules() {
		return authMiddleware
	}
	return func(next http.Handler) http.Handler {
		authed := authMiddleware(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Admin API is never publicly accessible. Check BEFORE the
			// glob matcher so a poorly-written pattern can't open it.
			if strings.HasPrefix(r.URL.Path, adminPrefix) {
				authed.ServeHTTP(w, r)
				return
			}
			if m.IsPubliclyReadable(r.Method, r.URL.Path) {
				ctx := context.WithValue(r.Context(), ctxPublic, true)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			authed.ServeHTTP(w, r)
		})
	}
}
