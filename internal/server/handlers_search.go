package server

import (
	"net/http"
	"strconv"
	"strings"
)

// handleSearch runs a full-text query against the page index and returns
// ranked hits. Empty / missing `q` returns an empty list — callers can poll
// the endpoint while the user types without special-casing "no input yet".
//
//	GET /api/search?q=how+does+grab+work&limit=10
//	GET /api/search?q=blog+post&tags=voice,content
//	GET /api/search?tags=runbook           (tags-only query)
//	→ 200 [{ path, title, summary, tags, snippet, rank, writer, updated_at }]
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if s.Search == nil {
		// FTS unavailable — return an empty result instead of 503 so
		// callers aren't forced to branch on a missing-feature case.
		respondJSON(w, http.StatusOK, []any{})
		return
	}

	q := r.URL.Query().Get("q")
	var tags []string
	if raw := r.URL.Query().Get("tags"); raw != "" {
		for _, t := range strings.Split(raw, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}

	hits, err := s.Search.Query(q, tags, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, hits)
}
