package server

import (
	"io"
	"net/http"
	"strings"
)

func (s *Server) handleListPages(w http.ResponseWriter, r *http.Request) {
	pages := s.Pages.ListPages()
	respondJSON(w, http.StatusOK, pages)
}

func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimPrefix(r.URL.Path, "/api/pages/")
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
	pagePath := strings.TrimPrefix(r.URL.Path, "/api/pages/")
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
	pagePath := strings.TrimPrefix(r.URL.Path, "/api/pages/")

	if pagePath == "index" || pagePath == "index.md" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "cannot delete index page")
		return
	}

	if err := s.Pages.DeletePage(pagePath); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
