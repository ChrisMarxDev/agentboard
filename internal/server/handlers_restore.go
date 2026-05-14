package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
)

// POST /_api/restore — re-commits the historical content of `path`
// at `sha` as a new commit. Cookie-auth + CSRF gated, same as
// /_api/edit. Used by the "(restore)" button in the history view.
//
// Body (form-encoded): path, sha, csrf.

// RestoreFn is the bridge: read the file at `sha`, then write it back
// as a new commit on the default branch. cli/serve.go wires the
// concrete implementation; tests can swap.
type RestoreFn func(ctx context.Context, workspace, path, sha, actor string) error

func (s *Server) handleRestoreSubmit(w http.ResponseWriter, r *http.Request) {
	if s.RestoreFn == nil {
		respondError(w, http.StatusServiceUnavailable, "unavailable", "restore not configured")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		respondError(w, http.StatusUnauthorized, "unauthorized", "sign in to restore")
		return
	}
	if err := r.ParseForm(); err != nil {
		respondError(w, http.StatusBadRequest, "bad_request", "invalid form")
		return
	}
	formCSRF := r.FormValue("csrf")
	cookie, err := r.Cookie("agentboard_csrf")
	if err != nil || cookie.Value == "" || formCSRF == "" || cookie.Value != formCSRF {
		respondError(w, http.StatusForbidden, "csrf_mismatch", "CSRF check failed; reload the form and try again")
		return
	}
	path := strings.TrimSpace(r.FormValue("path"))
	sha := strings.TrimSpace(r.FormValue("sha"))
	if path == "" || sha == "" {
		respondError(w, http.StatusBadRequest, "bad_request", "path and sha required")
		return
	}
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
		respondError(w, http.StatusBadRequest, "bad_request", "invalid path")
		return
	}
	if err := s.RestoreFn(r.Context(), "dogfood", path, sha, user.Username); err != nil {
		respondError(w, http.StatusInternalServerError, "restore_failed", err.Error())
		return
	}
	http.Redirect(w, r, "/"+path, http.StatusFound)
}
