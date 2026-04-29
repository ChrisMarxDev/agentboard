package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/inbox"
)

// handleListInbox returns recent items for the current user. The
// recipient is always the caller — no cross-user reads, even for
// admins. Privacy boundary is strong.
//
//	GET /api/inbox?unread=true&limit=50
func (s *Server) handleListInbox(w http.ResponseWriter, r *http.Request) {
	if s.Inbox == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "inbox unavailable")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "sign in to read inbox")
		return
	}
	unread := r.URL.Query().Get("unread") == "true"
	limit := 50
	if lv := r.URL.Query().Get("limit"); lv != "" {
		if n, err := strconv.Atoi(lv); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	items, err := s.Inbox.List(inbox.ListParams{
		Recipient:  user.Username,
		UnreadOnly: unread,
		Limit:      limit,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	// Always return [] not null on empty so the SPA can rely on the
	// shape and not stay stuck on "Loading inbox…" — useState<null>
	// is the SPA's loading sentinel, and a JSON `null` from this
	// endpoint round-trips into that state.
	if items == nil {
		items = []*inbox.Item{}
	}
	respondJSON(w, http.StatusOK, items)
}

// handleInboxCount returns unread count + total count. Suitable for
// nav badges that poll on mount + every ~30s.
//
//	GET /api/inbox/count
func (s *Server) handleInboxCount(w http.ResponseWriter, r *http.Request) {
	if s.Inbox == nil {
		respondJSON(w, http.StatusOK, map[string]int{"unread": 0})
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		respondJSON(w, http.StatusOK, map[string]int{"unread": 0})
		return
	}
	n, err := s.Inbox.UnreadCount(user.Username)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"unread": n})
}

// handleInboxMarkAllRead marks every unread item read.
//
//	POST /api/inbox/read-all
func (s *Server) handleInboxMarkAllRead(w http.ResponseWriter, r *http.Request) {
	if s.Inbox == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "inbox unavailable")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "sign in")
		return
	}
	n, err := s.Inbox.MarkAllRead(user.Username)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "marked": n})
}

// handleInboxItem dispatches mark-read / archive / delete on a single
// item, based on the HTTP method + a small action verb in the URL.
//
//	POST   /api/inbox/{id}/read
//	POST   /api/inbox/{id}/archive
//	DELETE /api/inbox/{id}
func (s *Server) handleInboxItem(w http.ResponseWriter, r *http.Request) {
	if s.Inbox == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "inbox unavailable")
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "sign in")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/inbox/")
	parts := strings.SplitN(rest, "/", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_ID", "numeric id required")
		return
	}
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	var opErr error
	switch {
	case r.Method == http.MethodDelete:
		opErr = s.Inbox.Delete(id, user.Username)
	case r.Method == http.MethodPost && action == "read":
		opErr = s.Inbox.MarkRead(id, user.Username)
	case r.Method == http.MethodPost && action == "archive":
		opErr = s.Inbox.Archive(id, user.Username)
	default:
		respondError(w, http.StatusBadRequest, "INVALID_ACTION", "expected POST /read, POST /archive, or DELETE")
		return
	}
	if opErr != nil {
		switch {
		case errors.Is(opErr, inbox.ErrNotFound):
			respondError(w, http.StatusNotFound, "NOT_FOUND", "inbox item not found")
		case errors.Is(opErr, inbox.ErrForbidden):
			respondError(w, http.StatusForbidden, "FORBIDDEN", "not your inbox item")
		default:
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", opErr.Error())
		}
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- helper used only by this handler set ----

var _ = json.NewEncoder // silence import when no other use
