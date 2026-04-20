package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// authMiddleware gates every route behind a shared token when one is configured.
// When token is empty the middleware is a no-op, preserving the default
// loopback-only posture for local `task run` / `task dev`.
//
// Accepted token sources:
//   - Authorization: Bearer <token>   (API/MCP clients)
//   - HTTP Basic Auth, password=token (browsers — triggered by 401 + WWW-Authenticate)
//   - ?token=<token> query parameter  (one-click share links / EventSource bootstrap)
//
// /api/health stays open so platform health probes don't need the secret.
// OPTIONS preflights pass through so CORS continues to work.
func authMiddleware(token string) func(http.Handler) http.Handler {
	expected := []byte(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodOptions || r.URL.Path == "/api/health" {
				next.ServeHTTP(w, r)
				return
			}

			if ah := r.Header.Get("Authorization"); ah != "" {
				if rest, ok := strings.CutPrefix(ah, "Bearer "); ok {
					if subtle.ConstantTimeCompare([]byte(rest), expected) == 1 {
						next.ServeHTTP(w, r)
						return
					}
				}
				if _, pw, ok := r.BasicAuth(); ok {
					if subtle.ConstantTimeCompare([]byte(pw), expected) == 1 {
						next.ServeHTTP(w, r)
						return
					}
				}
			}

			if qt := r.URL.Query().Get("token"); qt != "" {
				if subtle.ConstantTimeCompare([]byte(qt), expected) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}

			w.Header().Set("WWW-Authenticate", `Basic realm="agentboard"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
}
