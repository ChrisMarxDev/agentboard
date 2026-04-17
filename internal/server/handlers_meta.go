package server

import (
	"net/http"
)

const versionStr = "0.1.0"

// Version returns the current version string.
func Version() string {
	return versionStr
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"version": versionStr,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, s.Project.Config)
}

func (s *Server) handleSkill(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(s.SkillFile))
}
