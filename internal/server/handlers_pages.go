package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/christophermarx/agentboard/internal/mdx"
)

// respondPageStale emits a 412 with the current page info so the caller can
// re-base and retry. page may be nil when the path doesn't exist.
func respondPageStale(w http.ResponseWriter, page *mdx.PageInfo, path string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPreconditionFailed)
	payload := map[string]any{
		"code":  "STALE_WRITE",
		"error": "If-Match did not match current page etag",
		"path":  path,
	}
	if page != nil {
		payload["current"] = page
	}
	_ = json.NewEncoder(w).Encode(payload)
}

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

	// Content-addressed etag — round-trip as If-Match on the caller's next write.
	if page.Etag != "" {
		w.Header().Set("ETag", `"`+page.Etag+`"`)
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

	expected := ifMatch(r)
	writeErr := s.Pages.WritePageIfMatch(pagePath, string(body), expected)
	if writeErr != nil {
		if errors.Is(writeErr, mdx.ErrStale) || errors.Is(writeErr, mdx.ErrNotFoundForMatch) {
			respondPageStale(w, s.Pages.GetPage(strings.TrimSuffix(pagePath, ".md")), pagePath)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", writeErr.Error())
		return
	}

	// Broadcast page update via SSE
	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-updated",
		Data: []byte(`{"path":"` + pagePath + `"}`),
	})

	// Echo the new etag so clients don't need a follow-up GET.
	page := s.Pages.GetPage(strings.TrimSuffix(pagePath, ".md"))
	resp := map[string]any{"ok": true, "compiled": true}
	if page != nil {
		resp["etag"] = page.Etag
		w.Header().Set("ETag", `"`+page.Etag+`"`)
	}
	respondJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeletePage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimPrefix(r.URL.Path, "/api/content/")

	if pagePath == "index" || pagePath == "index.md" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "cannot delete index page")
		return
	}

	expected := ifMatch(r)
	delErr := s.Pages.DeletePageIfMatch(pagePath, expected)
	if delErr != nil {
		if errors.Is(delErr, mdx.ErrStale) || errors.Is(delErr, mdx.ErrNotFoundForMatch) {
			respondPageStale(w, s.Pages.GetPage(strings.TrimSuffix(pagePath, ".md")), pagePath)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", delErr.Error())
		return
	}

	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-updated",
		Data: []byte(`{"path":"` + pagePath + `","deleted":true}`),
	})

	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
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
