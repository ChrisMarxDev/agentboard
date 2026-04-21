package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
)

func (s *Server) handleListPages(w http.ResponseWriter, r *http.Request) {
	pages := s.Pages.ListPages()
	respondJSON(w, http.StatusOK, pages)
}

func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimPrefix(r.URL.Path, "/api/content/")
	if pagePath == "" {
		pagePath = "index"
	}

	page := s.Pages.GetPage(pagePath)
	if page == nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "page not found: "+pagePath)
		return
	}

	// Check Accept header to determine response format
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/markdown") {
		w.Header().Set("Content-Type", "text/markdown")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(page.Source))
		return
	}

	// Default: return page metadata + source
	respondJSON(w, http.StatusOK, page)
}

func (s *Server) handleWritePage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimPrefix(r.URL.Path, "/api/content/")
	if pagePath == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "page path required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}

	if err := s.Pages.WritePage(pagePath, string(body)); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Broadcast page update via SSE
	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-updated",
		Data: []byte(`{"path":"` + pagePath + `"}`),
	})

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"compiled": true,
	})
}

func (s *Server) handleDeletePage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimPrefix(r.URL.Path, "/api/content/")

	if pagePath == "index" || pagePath == "index.md" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "cannot delete index page")
		return
	}

	if err := s.Pages.DeletePage(pagePath); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-updated",
		Data: []byte(`{"path":"` + pagePath + `","deleted":true}`),
	})

	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// handleMovePage renames/moves a page file. Body: {"from": "...", "to": "..."}.
// Returns 404 if the source doesn't exist, 409 if the destination does, 400
// for invalid paths (empty, index, or containing "..").
func (s *Server) handleMovePage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "expected JSON body {from, to}")
		return
	}

	from := normalizePagePath(body.From)
	to := normalizePagePath(body.To)

	if from == "" || to == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "both from and to are required")
		return
	}
	if from == "index" || to == "index" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "cannot move to or from the index page")
		return
	}
	if strings.Contains(from, "..") || strings.Contains(to, "..") {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "path must not contain '..'")
		return
	}
	if from == to {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "from and to must differ")
		return
	}

	if err := s.Pages.MovePage(from, to); err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			respondError(w, http.StatusNotFound, "NOT_FOUND", "source page does not exist: "+from)
		case errors.Is(err, os.ErrExist):
			respondError(w, http.StatusConflict, "CONFLICT", "destination already exists: "+to)
		default:
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
		return
	}

	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-updated",
		Data: []byte(`{"from":"` + from + `","to":"` + to + `","moved":true}`),
	})

	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "from": from, "to": to})
}

// normalizePagePath trims a leading slash, trailing ".md", and surrounding
// whitespace so inputs like "/features/old", "features/old.md", or " features/old "
// all resolve to the canonical "features/old".
func normalizePagePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, ".md")
	return p
}
