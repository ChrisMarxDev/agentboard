package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/locks"
	"github.com/go-chi/chi/v5"
)

// Page locks — admin-only "freeze this page against non-admin edits".
// Distinct from approval: lock restricts future edits; approval records
// that a specific version was reviewed.
//
// Route surface:
//   GET    /api/locks              — any authed user (locks are broadcast info)
//   POST   /api/locks              — admin only; body {path, reason?}
//   DELETE /api/locks/*            — admin only; path is everything after /api/locks/

type createLockRequest struct {
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`
}

// registerLockRoutes wires /api/locks. Called from apiRoutes.
func (s *Server) registerLockRoutes(r chi.Router) {
	r.Get("/locks", s.handleListLocks)
	r.Group(func(r chi.Router) {
		r.Use(auth.AdminRequired())
		r.Post("/locks", s.handleCreateLock)
		r.Delete("/locks/*", s.handleDeleteLock)
	})
}

func (s *Server) handleListLocks(w http.ResponseWriter, r *http.Request) {
	if s.Locks == nil {
		respondJSON(w, 200, map[string]any{"locks": []any{}})
		return
	}
	list, err := s.Locks.List()
	if err != nil {
		respondError(w, 500, "internal", "list failed")
		return
	}
	respondJSON(w, 200, map[string]any{"locks": list})
}

func (s *Server) handleCreateLock(w http.ResponseWriter, r *http.Request) {
	if s.Locks == nil {
		respondError(w, 503, "unavailable", "locks store unavailable")
		return
	}
	var req createLockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		respondError(w, 400, "bad_request", "path required")
		return
	}
	// The path must point at an existing page. Defence against locking
	// a ghost path and preventing the future page from ever being
	// created by a non-admin.
	normalized := strings.TrimPrefix(strings.TrimSuffix(path, ".md"), "/")
	if normalized == "" || s.Pages.GetPage(normalized) == nil {
		respondError(w, 404, "not_found", "page does not exist")
		return
	}
	actor := ""
	if caller := auth.UserFromContext(r.Context()); caller != nil {
		actor = caller.Username
	}
	lock, err := s.Locks.Lock(locks.LockParams{
		Path:     normalized,
		LockedBy: actor,
		Reason:   req.Reason,
	})
	if err != nil {
		respondError(w, 500, "internal", "lock failed")
		return
	}
	respondJSON(w, 201, lock)
}

func (s *Server) handleDeleteLock(w http.ResponseWriter, r *http.Request) {
	if s.Locks == nil {
		respondError(w, 503, "unavailable", "locks store unavailable")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/locks/")
	if err := s.Locks.Unlock(path); err != nil {
		if errors.Is(err, locks.ErrNotFound) {
			respondError(w, 404, "not_found", "page is not locked")
			return
		}
		respondError(w, 500, "internal", "unlock failed")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "unlocked"})
}

// -------- write-path enforcement --------

// enforcePageLock returns a non-nil error if `path` is locked AND the
// caller isn't admin-kind. Call BEFORE any page write / delete / move.
// Errors manifest as 403 with a structured body so the frontend can
// distinguish a lock from a generic auth failure.
//
// The caller is read from request context — never from a body field.
// Anonymous readers don't pass through this gate (they never reach
// write handlers).
func (s *Server) enforcePageLock(r *http.Request, path string) *lockedError {
	if s.Locks == nil {
		return nil
	}
	caller := auth.UserFromContext(r.Context())
	if caller != nil && caller.Kind == auth.KindAdmin {
		return nil
	}
	lock, err := s.Locks.Get(path)
	if err != nil || lock == nil {
		return nil
	}
	return &lockedError{lock: lock}
}

// lockedError carries the lock metadata so the caller can serialize
// into the 403 response body.
type lockedError struct {
	lock *locks.Lock
}

// respondPageLocked writes a structured 403 with the lock metadata the
// frontend needs to render the "Locked by @X" UI.
func respondPageLocked(w http.ResponseWriter, e *lockedError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":     "page is locked — contact an admin",
		"code":      "PAGE_LOCKED",
		"locked_by": e.lock.LockedBy,
		"locked_at": e.lock.LockedAt,
		"reason":    e.lock.Reason,
	})
}
