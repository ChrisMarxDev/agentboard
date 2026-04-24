package mcp

import (
	"encoding/json"
	"errors"

	"github.com/christophermarx/agentboard/internal/teams"
)

// Team MCP tools. Agents with an admin token can create/manage teams;
// agent tokens can only list/read. The MCP layer doesn't enforce this
// directly — it relies on the bearer token the tool-call rides on,
// which is checked by the surrounding HTTP middleware when the call
// hits /api/admin/teams for write ops. Here we just expose the surface.

// toolListTeams → agentboard_list_teams
func (s *Server) toolListTeams() (interface{}, *RPCError) {
	if s.Teams == nil {
		return nil, &RPCError{Code: -32000, Message: "teams unavailable"}
	}
	list, err := s.Teams.ListWithMembers()
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return map[string]any{"teams": list}, nil
}

// toolGetTeam → agentboard_get_team
func (s *Server) toolGetTeam(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Teams == nil {
		return nil, &RPCError{Code: -32000, Message: "teams unavailable"}
	}
	slug := getString(args, "slug")
	if slug == "" {
		return nil, &RPCError{Code: -32602, Message: "slug required"}
	}
	t, err := s.Teams.Get(slug)
	if errors.Is(err, teams.ErrNotFound) {
		return nil, &RPCError{Code: -32000, Message: "team not found"}
	}
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return t, nil
}

// toolCreateTeam → agentboard_create_team
func (s *Server) toolCreateTeam(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Teams == nil {
		return nil, &RPCError{Code: -32000, Message: "teams unavailable"}
	}
	slug := getString(args, "slug")
	if slug == "" {
		return nil, &RPCError{Code: -32602, Message: "slug required"}
	}
	actor := ""
	if s.ActorResolver != nil {
		actor = s.ActorResolver()
	}
	t, err := s.Teams.Create(teams.CreateParams{
		Slug:        slug,
		DisplayName: getString(args, "display_name"),
		Description: getString(args, "description"),
		CreatedBy:   actor,
	})
	if errors.Is(err, teams.ErrSlugTaken) {
		return nil, &RPCError{Code: -32000, Message: "slug already in use"}
	}
	if errors.Is(err, teams.ErrInvalidSlug) || errors.Is(err, teams.ErrReservedSlug) {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	// Optional initial members.
	if raw, ok := args["members"]; ok && len(raw) > 0 {
		var members []string
		_ = json.Unmarshal(raw, &members)
		for _, u := range members {
			_ = s.Teams.AddMember(teams.AddMemberParams{Slug: t.Slug, Username: u})
		}
	}
	full, _ := s.Teams.Get(t.Slug)
	return full, nil
}

// toolDeleteTeam → agentboard_delete_team
func (s *Server) toolDeleteTeam(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Teams == nil {
		return nil, &RPCError{Code: -32000, Message: "teams unavailable"}
	}
	slug := getString(args, "slug")
	if slug == "" {
		return nil, &RPCError{Code: -32602, Message: "slug required"}
	}
	err := s.Teams.Delete(slug)
	if errors.Is(err, teams.ErrNotFound) {
		return nil, &RPCError{Code: -32000, Message: "team not found"}
	}
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return map[string]any{"ok": true, "slug": slug}, nil
}

// toolAddTeamMember → agentboard_add_team_member
func (s *Server) toolAddTeamMember(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Teams == nil {
		return nil, &RPCError{Code: -32000, Message: "teams unavailable"}
	}
	slug := getString(args, "slug")
	username := getString(args, "username")
	if slug == "" || username == "" {
		return nil, &RPCError{Code: -32602, Message: "slug and username required"}
	}
	err := s.Teams.AddMember(teams.AddMemberParams{
		Slug:     slug,
		Username: username,
		Role:     getString(args, "role"),
	})
	if errors.Is(err, teams.ErrNotFound) {
		return nil, &RPCError{Code: -32000, Message: "team not found"}
	}
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	t, _ := s.Teams.Get(slug)
	return t, nil
}

// toolRemoveTeamMember → agentboard_remove_team_member
func (s *Server) toolRemoveTeamMember(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Teams == nil {
		return nil, &RPCError{Code: -32000, Message: "teams unavailable"}
	}
	slug := getString(args, "slug")
	username := getString(args, "username")
	if slug == "" || username == "" {
		return nil, &RPCError{Code: -32602, Message: "slug and username required"}
	}
	err := s.Teams.RemoveMember(slug, username)
	if errors.Is(err, teams.ErrNotMember) {
		return nil, &RPCError{Code: -32000, Message: "user is not a member"}
	}
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	t, _ := s.Teams.Get(slug)
	return t, nil
}
