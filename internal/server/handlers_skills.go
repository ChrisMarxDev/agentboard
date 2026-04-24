package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/files"
	"github.com/go-chi/chi/v5"
)

// handleListSkills returns the index of every folder under content/skills/
// that has a valid SKILL.md. Folders without a manifest or with malformed
// frontmatter are silently skipped — skills is a read-view on top of generic
// file storage, not a separate store.
func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "files manager not configured")
		return
	}
	list, err := s.Files.ListSkills()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, list)
}

// handleGetSkill streams a zip of the skill folder. Unzipping the archive
// yields a <slug>/ folder containing SKILL.md and any supporting files.
func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "files manager not configured")
		return
	}
	slug := chi.URLParam(r, "slug")
	if slug == "" || strings.ContainsAny(slug, "/\\") {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "invalid skill slug")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+slug+`.zip"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")

	err := s.Files.WriteSkillZip(slug, w)
	switch {
	case errors.Is(err, files.ErrInvalidName):
		// Header may already be partially flushed; safe to attempt an error response
		// since we haven't written any body bytes until the first zip entry.
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "invalid skill slug")
		return
	case errors.Is(err, files.ErrSkillNotFound):
		respondError(w, http.StatusNotFound, "NOT_FOUND", "skill not found: "+slug)
		return
	case err != nil:
		// Zip writer may have flushed a partial response; log and return.
		// We can't safely write JSON at this point.
		return
	}
}
