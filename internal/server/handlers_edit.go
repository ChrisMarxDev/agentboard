package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
)

// POST /_api/edit — accepts form-encoded {path, body, message, csrf}
// from the in-browser editor surfaced by html.Server.renderEdit.
// Commits via the EditFn callback (a thin bridge into gitserver.PutFile).
//
// Auth: session cookie required. Bearer-authenticated callers should
// hit gitserver directly via git push or MCP agentboard_propose; this
// endpoint exists for the human-facing edit-in-browser flow.
//
// CSRF: double-submit cookie + form field. The form embeds the
// agentboard_csrf cookie value as a hidden field; we compare here.

// EditFn is the bridge to gitserver.PutFile. cli/serve.go wires the
// concrete implementation; tests can swap a fake in.
type EditFn func(ctx context.Context, workspace, path, body, actor, message string) error

func (s *Server) handleEditSubmit(w http.ResponseWriter, r *http.Request) {
	if s.EditFn == nil {
		respondError(w, http.StatusServiceUnavailable, "unavailable", "edit not configured")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		respondError(w, http.StatusUnauthorized, "unauthorized", "sign in to edit")
		return
	}
	if err := r.ParseForm(); err != nil {
		respondError(w, http.StatusBadRequest, "bad_request", "invalid form")
		return
	}
	// Double-submit CSRF check: form field must match cookie.
	formCSRF := r.FormValue("csrf")
	cookie, err := r.Cookie("agentboard_csrf")
	if err != nil || cookie.Value == "" || formCSRF == "" || cookie.Value != formCSRF {
		respondError(w, http.StatusForbidden, "csrf_mismatch", "CSRF check failed; reload the form and try again")
		return
	}
	path := strings.TrimSpace(r.FormValue("path"))
	body := r.FormValue("body")
	message := strings.TrimSpace(r.FormValue("message"))
	if path == "" {
		respondError(w, http.StatusBadRequest, "bad_request", "path required")
		return
	}
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
		respondError(w, http.StatusBadRequest, "bad_request", "invalid path")
		return
	}
	if message == "" {
		message = "Edit " + path
	}
	if err := s.EditFn(r.Context(), "dogfood", path, body, user.Username, message); err != nil {
		respondError(w, http.StatusInternalServerError, "commit_failed", err.Error())
		return
	}
	// Success → redirect back to the rendered file.
	http.Redirect(w, r, "/"+path, http.StatusFound)
}
