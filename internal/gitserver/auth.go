package gitserver

import (
	"net/http"

	"github.com/christophermarx/agentboard/internal/auth"
)

// userFromRequest returns the authenticated username on the request
// context (set by auth.TokenMiddleware), or empty string and ok=false
// when no user is attached. Kept as a tiny wrapper so the rest of
// the package doesn't reach into the auth package directly.
func userFromRequest(r *http.Request) (string, bool) {
	u := auth.UserFromContext(r.Context())
	if u == nil || u.Username == "" {
		return "", false
	}
	return u.Username, true
}
