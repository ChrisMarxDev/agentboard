package server

import (
	"net/http"

	"github.com/christophermarx/agentboard/internal/auth"
)

// POST /_api/preview — renders a Markdown body via the same goldmark
// pipeline the dashboard uses inline, returning the rendered HTML
// (no shell). Used by the Markdown editor's live preview pane
// (html.Server.renderEdit emits a fetch() loop that posts here on
// every debounced keystroke).
//
// Cookie-auth + CSRF gated. No write occurs, but we still scope to
// signed-in users so anonymous traffic can't use the endpoint as a
// free goldmark service.

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if s.PreviewFn == nil {
		respondError(w, http.StatusServiceUnavailable, "unavailable", "preview not configured")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		respondError(w, http.StatusUnauthorized, "unauthorized", "sign in to preview")
		return
	}
	if err := r.ParseForm(); err != nil {
		respondError(w, http.StatusBadRequest, "bad_request", "invalid form")
		return
	}
	// CSRF: the editor JS sends the token in both the X-CSRF-Token
	// header (which the cookie-CSRF middleware checks upstream) and
	// the form field. Header check has already happened; the form-
	// field check here is belt-and-suspenders.
	formCSRF := r.FormValue("csrf")
	cookie, err := r.Cookie("agentboard_csrf")
	if err != nil || cookie.Value == "" || formCSRF == "" || cookie.Value != formCSRF {
		respondError(w, http.StatusForbidden, "csrf_mismatch", "CSRF check failed; reload the form and try again")
		return
	}
	html, err := s.PreviewFn([]byte(r.FormValue("body")))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "preview_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// No-cache: the preview is per-keystroke; nothing to cache.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(html))
}
