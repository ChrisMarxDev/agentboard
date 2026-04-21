package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/grab"
)

// grabRequest is the POST /api/grab body.
type grabRequest struct {
	Picks  []grab.Pick `json:"picks"`
	Format string      `json:"format"`
}

// grabResponse is what the endpoint returns.
type grabResponse struct {
	Format   string         `json:"format"`
	Text     string         `json:"text"`
	Sections []grab.Section `json:"sections"`
}

func (s *Server) handleGrab(w http.ResponseWriter, r *http.Request) {
	if s.Grab == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "grab materializer not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024))
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}
	var req grabRequest
	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body is not valid JSON")
		return
	}
	if len(req.Picks) == 0 {
		respondJSON(w, http.StatusOK, grabResponse{
			Format:   "markdown",
			Text:     "",
			Sections: []grab.Section{},
		})
		return
	}

	format := grab.Format(strings.ToLower(strings.TrimSpace(req.Format)))
	if format != grab.FormatXML && format != grab.FormatJSON {
		format = grab.FormatMarkdown
	}

	sections := s.Grab.Materialize(req.Picks)
	text := grab.Render(sections, format)

	respondJSON(w, http.StatusOK, grabResponse{
		Format:   string(format),
		Text:     text,
		Sections: sections,
	})
}
