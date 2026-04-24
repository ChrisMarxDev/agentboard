package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/teams"
	"github.com/go-chi/chi/v5"
)

// Team admin endpoints. Mount order:
//
//	/api/admin/teams              (admin-gated; create/delete/member-ops)
//	/api/teams                    (agent-readable; list + single read)
//
// The read-side lives outside /api/admin so agents can populate a
// "who's on marketing?" prompt without needing an admin token.

type createTeamRequest struct {
	Slug        string   `json:"slug"`
	DisplayName string   `json:"display_name,omitempty"`
	Description string   `json:"description,omitempty"`
	Members     []string `json:"members,omitempty"`
}

type updateTeamRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type addMemberRequest struct {
	Username string `json:"username"`
	Role     string `json:"role,omitempty"`
}

// registerTeamRoutes wires the agent-read surface (/api/teams) AND
// delegates admin-gated writes to registerAdminTeamRoutes. Called from
// apiRoutes.
func (s *Server) registerTeamRoutes(r chi.Router) {
	r.Get("/teams", s.handleListTeams)
	r.Get("/teams/{slug}", s.handleGetTeam)
}

// registerAdminTeamRoutes is invoked from registerAdminRoutes; gates
// create/update/delete + member-ops behind admin.
func (s *Server) registerAdminTeamRoutes(r chi.Router) {
	r.Route("/teams", func(r chi.Router) {
		r.Post("/", s.handleCreateTeam)
		r.Route("/{slug}", func(r chi.Router) {
			r.Patch("/", s.handleUpdateTeam)
			r.Delete("/", s.handleDeleteTeam)
			r.Post("/members", s.handleAddTeamMember)
			r.Delete("/members/{username}", s.handleRemoveTeamMember)
		})
	})
}

// -------- reads --------

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	if s.Teams == nil {
		respondJSON(w, 200, map[string]any{"teams": []any{}})
		return
	}
	teams, err := s.Teams.ListWithMembers()
	if err != nil {
		respondError(w, 500, "internal", "list failed")
		return
	}
	out := make([]any, 0, len(teams))
	for _, t := range teams {
		out = append(out, t)
	}
	respondJSON(w, 200, map[string]any{"teams": out})
}

func (s *Server) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	if s.Teams == nil {
		respondError(w, 404, "not_found", "teams unavailable")
		return
	}
	slug := strings.ToLower(chi.URLParam(r, "slug"))
	t, err := s.Teams.Get(slug)
	if errors.Is(err, teams.ErrNotFound) {
		respondError(w, 404, "not_found", "team not found")
		return
	}
	if err != nil {
		respondError(w, 500, "internal", "get failed")
		return
	}
	respondJSON(w, 200, t)
}

// -------- writes (admin only) --------

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	if s.Teams == nil {
		respondError(w, 503, "unavailable", "teams store not wired")
		return
	}
	var req createTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	// Users win the slug space over teams — if a user already has this
	// slug, refuse creation so mentions of @<slug> keep referring to
	// the person.
	if s.Auth != nil {
		if existing, _ := s.Auth.GetUser(slug); existing != nil {
			respondError(w, 409, "slug_taken", "a user already owns this slug")
			return
		}
	}
	actor := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		actor = u.Username
	}
	t, err := s.Teams.Create(teams.CreateParams{
		Slug:        slug,
		DisplayName: req.DisplayName,
		Description: req.Description,
		CreatedBy:   actor,
	})
	if errors.Is(err, teams.ErrSlugTaken) {
		respondError(w, 409, "slug_taken", "team already exists")
		return
	}
	if errors.Is(err, teams.ErrInvalidSlug) || errors.Is(err, teams.ErrReservedSlug) {
		respondError(w, 400, "bad_slug", err.Error())
		return
	}
	if err != nil {
		respondError(w, 500, "internal", "create failed")
		return
	}
	// Optional initial members.
	for _, raw := range req.Members {
		u := strings.ToLower(strings.TrimSpace(raw))
		if u == "" {
			continue
		}
		// Verify user exists before adding; silently skip unknown ones.
		if s.Auth != nil {
			if existing, _ := s.Auth.GetUser(u); existing == nil {
				continue
			}
		}
		_ = s.Teams.AddMember(teams.AddMemberParams{Slug: t.Slug, Username: u})
	}
	full, _ := s.Teams.Get(t.Slug)
	respondJSON(w, 201, full)
}

func (s *Server) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	if s.Teams == nil {
		respondError(w, 503, "unavailable", "teams store not wired")
		return
	}
	slug := strings.ToLower(chi.URLParam(r, "slug"))
	var req updateTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	t, err := s.Teams.Update(slug, teams.UpdateParams{
		DisplayName: req.DisplayName,
		Description: req.Description,
	})
	if errors.Is(err, teams.ErrNotFound) {
		respondError(w, 404, "not_found", "team not found")
		return
	}
	if err != nil {
		respondError(w, 500, "internal", "update failed")
		return
	}
	respondJSON(w, 200, t)
}

func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	if s.Teams == nil {
		respondError(w, 503, "unavailable", "teams store not wired")
		return
	}
	slug := strings.ToLower(chi.URLParam(r, "slug"))
	err := s.Teams.Delete(slug)
	if errors.Is(err, teams.ErrNotFound) {
		respondError(w, 404, "not_found", "team not found")
		return
	}
	if err != nil {
		respondError(w, 500, "internal", "delete failed")
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleAddTeamMember(w http.ResponseWriter, r *http.Request) {
	if s.Teams == nil {
		respondError(w, 503, "unavailable", "teams store not wired")
		return
	}
	slug := strings.ToLower(chi.URLParam(r, "slug"))
	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))
	if username == "" {
		respondError(w, 400, "bad_request", "username required")
		return
	}
	if s.Auth != nil {
		if existing, _ := s.Auth.GetUser(username); existing == nil {
			respondError(w, 404, "user_not_found", "no such user")
			return
		}
	}
	err := s.Teams.AddMember(teams.AddMemberParams{
		Slug:     slug,
		Username: username,
		Role:     req.Role,
	})
	if errors.Is(err, teams.ErrNotFound) {
		respondError(w, 404, "not_found", "team not found")
		return
	}
	if err != nil {
		respondError(w, 500, "internal", "add member failed")
		return
	}
	t, _ := s.Teams.Get(slug)
	respondJSON(w, 200, t)
}

func (s *Server) handleRemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	if s.Teams == nil {
		respondError(w, 503, "unavailable", "teams store not wired")
		return
	}
	slug := strings.ToLower(chi.URLParam(r, "slug"))
	username := strings.ToLower(chi.URLParam(r, "username"))
	err := s.Teams.RemoveMember(slug, username)
	if errors.Is(err, teams.ErrNotMember) {
		respondError(w, 404, "not_found", "user not in team")
		return
	}
	if err != nil {
		respondError(w, 500, "internal", "remove failed")
		return
	}
	t, _ := s.Teams.Get(slug)
	respondJSON(w, 200, t)
}
