package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/christophermarx/agentboard/internal/groups"
	"github.com/go-chi/chi/v5"
)

// /_api/admin/groups — admin-gated CRUD for named user groups. The
// permission system (WIKI_PIVOT.md §5.2) reads these via the resolver
// when a path matches a restrictive rule.
//
// Routes mounted in server.go under /_api/admin (the admin-gated
// chain). Errors map to:
//
//   - 400 invalid name / bad payload
//   - 404 group missing / user missing
//   - 409 name collision / member already exists (returned as 200 on
//     idempotent add — see AddMember)
//   - 500 anything else

// registerAdminGroupRoutes wires the group endpoints onto an
// admin-gated chi.Router. The caller is responsible for the
// AdminRequired() middleware above this in the chain.
func (s *Server) registerAdminGroupRoutes(r chi.Router) {
	r.Get("/groups", s.handleListGroups)
	r.Post("/groups", s.handleCreateGroup)
	r.Get("/groups/{name}", s.handleGetGroup)
	r.Patch("/groups/{name}", s.handleRenameGroup)
	r.Delete("/groups/{name}", s.handleDeleteGroup)
	r.Get("/groups/{name}/members", s.handleListGroupMembers)
	r.Post("/groups/{name}/members", s.handleAddGroupMember)
	r.Delete("/groups/{name}/members/{username}", s.handleRemoveGroupMember)
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	if s.Groups == nil {
		respondError(w, http.StatusServiceUnavailable, "GROUPS_DISABLED", "groups store not wired")
		return
	}
	gs, err := s.Groups.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"groups": gs})
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	if s.Groups == nil {
		respondError(w, http.StatusServiceUnavailable, "GROUPS_DISABLED", "groups store not wired")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	g, err := s.Groups.Create(r.Context(), body.Name, resolveActor(r))
	if err != nil {
		respondGroupError(w, err)
		return
	}
	respondJSON(w, http.StatusCreated, g)
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	if s.Groups == nil {
		respondError(w, http.StatusServiceUnavailable, "GROUPS_DISABLED", "groups store not wired")
		return
	}
	name := chi.URLParam(r, "name")
	g, err := s.Groups.Get(r.Context(), name)
	if err != nil {
		respondGroupError(w, err)
		return
	}
	members, err := s.Groups.Members(r.Context(), name)
	if err != nil {
		respondGroupError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"group": g, "members": members})
}

func (s *Server) handleRenameGroup(w http.ResponseWriter, r *http.Request) {
	if s.Groups == nil {
		respondError(w, http.StatusServiceUnavailable, "GROUPS_DISABLED", "groups store not wired")
		return
	}
	name := chi.URLParam(r, "name")
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if err := s.Groups.Rename(r.Context(), name, body.Name); err != nil {
		respondGroupError(w, err)
		return
	}
	g, _ := s.Groups.Get(r.Context(), body.Name)
	respondJSON(w, http.StatusOK, g)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if s.Groups == nil {
		respondError(w, http.StatusServiceUnavailable, "GROUPS_DISABLED", "groups store not wired")
		return
	}
	name := chi.URLParam(r, "name")
	if err := s.Groups.Delete(r.Context(), name); err != nil {
		respondGroupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	if s.Groups == nil {
		respondError(w, http.StatusServiceUnavailable, "GROUPS_DISABLED", "groups store not wired")
		return
	}
	name := chi.URLParam(r, "name")
	members, err := s.Groups.Members(r.Context(), name)
	if err != nil {
		respondGroupError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"members": members})
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	if s.Groups == nil {
		respondError(w, http.StatusServiceUnavailable, "GROUPS_DISABLED", "groups store not wired")
		return
	}
	name := chi.URLParam(r, "name")
	var body struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if err := s.Groups.AddMember(r.Context(), name, body.Username); err != nil {
		respondGroupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	if s.Groups == nil {
		respondError(w, http.StatusServiceUnavailable, "GROUPS_DISABLED", "groups store not wired")
		return
	}
	name := chi.URLParam(r, "name")
	username := chi.URLParam(r, "username")
	if err := s.Groups.RemoveMember(r.Context(), name, username); err != nil {
		respondGroupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// respondGroupError maps groups package errors to HTTP responses.
func respondGroupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, groups.ErrInvalidName):
		respondError(w, http.StatusBadRequest, "INVALID_NAME", err.Error())
	case errors.Is(err, groups.ErrNotFound):
		respondError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, groups.ErrNameTaken):
		respondError(w, http.StatusConflict, "NAME_TAKEN", err.Error())
	case errors.Is(err, groups.ErrUserUnknown):
		respondError(w, http.StatusNotFound, "USER_UNKNOWN", err.Error())
	case errors.Is(err, groups.ErrNotMember):
		respondError(w, http.StatusNotFound, "NOT_MEMBER", err.Error())
	default:
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
	}
}

// Compile-time guard that the handlers all match http.HandlerFunc.
var _ = func() {
	var s *Server
	var _ = []http.HandlerFunc{
		s.handleListGroups, s.handleCreateGroup, s.handleGetGroup,
		s.handleRenameGroup, s.handleDeleteGroup,
		s.handleListGroupMembers, s.handleAddGroupMember, s.handleRemoveGroupMember,
	}
	_ = context.Background
}
