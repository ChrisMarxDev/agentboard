package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/go-chi/chi/v5"
)

// Admin endpoints. Gated by auth.AdminRequired; by the time the handler
// runs, the attached user is known to be kind=admin.
//
// All user-addressed routes use :username — usernames are the primary key
// in the data model, so there's no separate id to plumb through.

type createUserRequest struct {
	Username          string          `json:"username"`
	DisplayName       string          `json:"display_name,omitempty"`
	Kind              auth.Kind       `json:"kind"`
	AccessMode        auth.AccessMode `json:"access_mode,omitempty"`
	Rules             []auth.Rule     `json:"rules,omitempty"`
	InitialTokenLabel string          `json:"initial_token_label,omitempty"`
}

// updateUserRequest covers the fields mutable via the web UI. Username is
// deliberately absent — to rename, use the CLI escape hatch.
type updateUserRequest struct {
	DisplayName *string          `json:"display_name,omitempty"`
	AccessMode  *auth.AccessMode `json:"access_mode,omitempty"`
	Rules       *[]auth.Rule     `json:"rules,omitempty"`
}

type createTokenRequest struct {
	Label string `json:"label,omitempty"`
}

type tokenResponse struct {
	Username string `json:"username"`
	TokenID  string `json:"token_id"`
	Label    string `json:"label,omitempty"`
	Token    string `json:"token"`
}

type meResponse struct {
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name,omitempty"`
	Kind        auth.Kind `json:"kind"`
	AvatarColor string    `json:"avatar_color,omitempty"`
}

func (s *Server) registerAdminRoutes(r chi.Router) {
	r.Route("/admin", func(r chi.Router) {
		r.Use(auth.AdminRequired())

		r.Get("/me", s.handleAdminMe)

		r.Get("/users", s.handleListUsers)
		r.Post("/users", s.handleCreateUser)
		r.Route("/users/{username}", func(r chi.Router) {
			r.Patch("/", s.handleUpdateUser)
			r.Post("/deactivate", s.handleDeactivateUser)
			r.Post("/tokens", s.handleCreateToken)
			r.Route("/tokens/{tokenId}", func(r chi.Router) {
				r.Post("/rotate", s.handleRotateToken)
				r.Post("/revoke", s.handleRevokeToken)
			})
		})

		// Shares — admin view over every unrevoked share token on the
		// instance. Revoke cascades to view_sessions (handled by the
		// existing /api/share/{id} DELETE handler).
		r.Get("/shares", s.handleAdminListShares)

		// Webhooks — every subscription instance-wide, revoked or not.
		r.Get("/webhooks", s.handleAdminListWebhooks)

		// Teams — create/update/delete + member ops.
		s.registerAdminTeamRoutes(r)
	})
}

// -------------------- me --------------------

func (s *Server) handleAdminMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		respondError(w, 401, "unauthorized", "no user")
		return
	}
	respondJSON(w, 200, meResponse{
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Kind:        user.Kind,
		AvatarColor: user.AvatarColor,
	})
}

// -------------------- users --------------------

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Auth.ListUsers(true)
	if err != nil {
		respondError(w, 500, "internal", "list failed")
		return
	}
	respondJSON(w, 200, map[string]any{"users": users})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	caller := auth.UserFromContext(r.Context())
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if req.Kind != auth.KindAgent && req.Kind != auth.KindAdmin {
		respondError(w, 400, "bad_request", "kind must be 'agent' or 'admin'")
		return
	}
	if req.AccessMode == "" {
		req.AccessMode = auth.ModeAllowAll
	}
	creator := ""
	if caller != nil {
		creator = caller.Username
	}
	user, err := s.Auth.CreateUser(auth.CreateUserParams{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Kind:        req.Kind,
		AccessMode:  req.AccessMode,
		Rules:       req.Rules,
		CreatedBy:   creator,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrUsernameTaken):
			respondError(w, 409, "username_taken", "a user with this username already exists (usernames are reserved forever, even after deactivation)")
			return
		case errors.Is(err, auth.ErrInvalidUsername):
			respondError(w, 400, "invalid_username", err.Error())
			return
		}
		respondError(w, 500, "internal", "create failed")
		return
	}

	token, err := auth.GenerateToken()
	if err != nil {
		respondError(w, 500, "internal", "token gen failed")
		return
	}
	label := req.InitialTokenLabel
	if label == "" {
		label = "initial"
	}
	tok, err := s.Auth.CreateToken(auth.CreateTokenParams{
		Username:  user.Username,
		TokenHash: auth.HashToken(token),
		Label:     label,
	})
	if err != nil {
		respondError(w, 500, "internal", "token persist failed")
		return
	}
	respondJSON(w, 201, tokenResponse{
		Username: user.Username,
		TokenID:  tok.ID,
		Label:    tok.Label,
		Token:    token,
	})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(chi.URLParam(r, "username"))
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	user, err := s.Auth.UpdateUser(username, auth.UpdateUserParams{
		DisplayName: req.DisplayName,
		AccessMode:  req.AccessMode,
		Rules:       req.Rules,
	})
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			respondError(w, 404, "not_found", "user not found")
			return
		}
		respondError(w, 500, "internal", "update failed")
		return
	}
	respondJSON(w, 200, user)
}

func (s *Server) handleDeactivateUser(w http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(chi.URLParam(r, "username"))
	caller := auth.UserFromContext(r.Context())
	if caller != nil && strings.EqualFold(caller.Username, username) {
		respondError(w, 400, "self_deactivate",
			"cannot deactivate yourself; create another admin first with `agentboard admin mint-admin`")
		return
	}
	if err := s.Auth.Deactivate(username); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			respondError(w, 404, "not_found", "user not found or already deactivated")
			return
		}
		respondError(w, 500, "internal", "deactivate failed")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "deactivated"})
}

// -------------------- tokens --------------------

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(chi.URLParam(r, "username"))
	if _, err := s.Auth.GetUser(username); err != nil {
		respondError(w, 404, "not_found", "user not found")
		return
	}
	var req createTokenRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	token, err := auth.GenerateToken()
	if err != nil {
		respondError(w, 500, "internal", "token gen failed")
		return
	}
	tok, err := s.Auth.CreateToken(auth.CreateTokenParams{
		Username:  username,
		TokenHash: auth.HashToken(token),
		Label:     req.Label,
	})
	if err != nil {
		respondError(w, 500, "internal", "token persist failed")
		return
	}
	respondJSON(w, 201, tokenResponse{
		Username: username,
		TokenID:  tok.ID,
		Label:    tok.Label,
		Token:    token,
	})
}

func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(chi.URLParam(r, "username"))
	tokenID := chi.URLParam(r, "tokenId")
	tok, err := s.Auth.GetToken(tokenID)
	if err != nil || !strings.EqualFold(tok.Username, username) {
		respondError(w, 404, "not_found", "token not found on this user")
		return
	}
	newToken, err := auth.GenerateToken()
	if err != nil {
		respondError(w, 500, "internal", "token gen failed")
		return
	}
	if err := s.Auth.RotateToken(tokenID, auth.HashToken(newToken)); err != nil {
		respondError(w, 500, "internal", "rotate failed")
		return
	}
	respondJSON(w, 200, tokenResponse{
		Username: tok.Username,
		TokenID:  tok.ID,
		Label:    tok.Label,
		Token:    newToken,
	})
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	tokenID := chi.URLParam(r, "tokenId")
	callerToken := auth.TokenFromContext(r.Context())
	if callerToken != nil && callerToken.ID == tokenID {
		respondError(w, 400, "self_revoke_token",
			"cannot revoke the token you're using; rotate it instead, or create another admin token first")
		return
	}
	if err := s.Auth.RevokeToken(tokenID); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			respondError(w, 404, "not_found", "token not found or already revoked")
			return
		}
		respondError(w, 500, "internal", "revoke failed")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "revoked"})
}
