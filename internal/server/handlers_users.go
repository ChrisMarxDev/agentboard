package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
)

// Agent-readable user directory. These endpoints live on /api/users so any
// valid token (agent or admin) can populate @mention autocomplete and
// resolve `assignees` arrays on cards/tasks. They never expose tokens.

// publicUser is the subset of User fields that's safe to serve to agents.
// Notably: no rules, no access_mode, no created_by, no tokens.
type publicUser struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Kind        string `json:"kind"`
	AvatarColor string `json:"avatar_color,omitempty"`
	Deactivated bool   `json:"deactivated,omitempty"`
}

func toPublic(u *auth.User) publicUser {
	return publicUser{
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Kind:        string(u.Kind),
		AvatarColor: u.AvatarColor,
		Deactivated: u.DeactivatedAt != nil,
	}
}

// handleListUsersPublic returns all users (minus sensitive fields) for
// @mention autocomplete and @-picker UIs. Active users first, deactivated
// dimmed and last.
func (s *Server) handleListUsersPublic(w http.ResponseWriter, r *http.Request) {
	users, err := s.Auth.ListUsers(false)
	if err != nil {
		respondError(w, 500, "internal", "list failed")
		return
	}
	out := make([]publicUser, 0, len(users))
	for _, u := range users {
		out = append(out, toPublic(u))
	}
	respondJSON(w, 200, map[string]any{"users": out})
}

type resolveRequest struct {
	Usernames []string `json:"usernames"`
}

// handleResolveUsernames takes a list of usernames and returns the ones
// that match. Unknown usernames are dropped silently — the caller decides
// whether a missing match means "show as plain text" or "flag as invalid".
//
// Used by RichText renderers (to turn @foo into a badge) and by card/task
// assignment validators.
func (s *Server) handleResolveUsernames(w http.ResponseWriter, r *http.Request) {
	var req resolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	// Normalise: lower-case, trim, drop empties, cap at a sensible upper
	// bound so a malformed client can't DoS the endpoint with 10M strings.
	seen := make(map[string]struct{}, len(req.Usernames))
	clean := make([]string, 0, len(req.Usernames))
	for _, raw := range req.Usernames {
		u := strings.ToLower(strings.TrimSpace(raw))
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		clean = append(clean, u)
		if len(clean) >= 256 {
			break
		}
	}
	users, err := s.Auth.ResolveUsernames(clean)
	if err != nil {
		respondError(w, 500, "internal", "resolve failed")
		return
	}
	out := make([]publicUser, 0, len(users))
	for _, u := range users {
		out = append(out, toPublic(u))
	}
	respondJSON(w, 200, map[string]any{"users": out})
}
