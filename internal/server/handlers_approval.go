package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleCreateApproval records an approval for a page at its current
// etag. The client sends only {path}; the server reads the canonical
// etag from the page manager — preventing a stale-etag race where a
// concurrent write could be approved for an older version.
//
// POST /api/approval  body: { "path": "/handbook" }
func (s *Server) handleCreateApproval(w http.ResponseWriter, r *http.Request) {
	if s.PageApproval == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "approval store not available")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body must be JSON {path}")
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "path required")
		return
	}
	storedPath := normaliseApprovalPath(body.Path)
	page := s.Pages.GetPage(strings.TrimSuffix(storedPath, ".md"))
	if page == nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "page not found: "+storedPath)
		return
	}
	actor := resolveActor(r)
	if actor == "anonymous" {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "approval requires a signed-in user")
		return
	}
	a, err := s.PageApproval.Approve(storedPath, actor, page.Etag)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	// SSE broadcast — the UI re-reads approval on every page load, but
	// bumping a page-updated event keeps subscribers current without a
	// dedicated approval event type.
	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-approval",
		Data: []byte(`{"path":"` + storedPath + `","approved":true}`),
	})
	respondJSON(w, http.StatusOK, a)
}

// handleGetApproval returns the approval record for a page, or null.
// The response includes `stale` when the stored etag no longer matches
// the current page etag — the UI uses this to show "approved at vX,
// edited since".
//
// GET /api/approval?path=/handbook
func (s *Server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	if s.PageApproval == nil {
		respondJSON(w, http.StatusOK, nil)
		return
	}
	raw := r.URL.Query().Get("path")
	if raw == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "?path= query param required")
		return
	}
	storedPath := normaliseApprovalPath(raw)
	a, err := s.PageApproval.Get(storedPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if a == nil {
		respondJSON(w, http.StatusOK, nil)
		return
	}
	// Attach the staleness flag by comparing against the current page etag.
	page := s.Pages.GetPage(strings.TrimSuffix(storedPath, ".md"))
	stale := false
	if page == nil || page.Etag != a.ApprovedEtag {
		stale = true
	}
	resp := map[string]any{
		"path":          a.Path,
		"approved_by":   a.ApprovedBy,
		"approved_at":   a.ApprovedAt,
		"approved_etag": a.ApprovedEtag,
		"stale":         stale,
	}
	respondJSON(w, http.StatusOK, resp)
}

// handleRevokeApproval deletes the approval for a page. Idempotent.
//
// DELETE /api/approval?path=/handbook
func (s *Server) handleRevokeApproval(w http.ResponseWriter, r *http.Request) {
	if s.PageApproval == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "approval store not available")
		return
	}
	raw := r.URL.Query().Get("path")
	if raw == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "?path= query param required")
		return
	}
	storedPath := normaliseApprovalPath(raw)
	if err := s.PageApproval.Revoke(storedPath); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-approval",
		Data: []byte(`{"path":"` + storedPath + `","approved":false}`),
	})
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// normaliseApprovalPath matches how GetPage keys its pages: strip any
// leading slash, collapse "/" to "index", trim a trailing ".md".
func normaliseApprovalPath(p string) string {
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, ".md")
	if p == "" {
		p = "index"
	}
	return p
}
