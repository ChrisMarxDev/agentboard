package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/go-chi/chi/v5"
)

// Per-user token management, scoped by auth.ScopeSelfOrAdmin.
//
//	GET    /api/users/{username}/tokens                  — list
//	POST   /api/users/{username}/tokens                  — mint a new token
//	POST   /api/users/{username}/tokens/{tokenId}/rotate — rotate (returns new plaintext)
//	POST   /api/users/{username}/tokens/{tokenId}/revoke — revoke
//
// Rule enforced by the middleware:
//   - kind(caller) == admin → allowed for any target.
//   - caller == target → allowed (members own themselves).
//   - Otherwise 403.
//
// This lets a member rotate their laptop token without involving an
// admin, while still letting admins manage bot tokens shared across
// the team.

func (s *Server) registerUserTokenRoutes(r chi.Router) {
	r.Route("/users/{username}/tokens", func(r chi.Router) {
		r.Use(auth.ScopeSelfOrAdmin(func(r *http.Request, name string) string {
			return chi.URLParam(r, name)
		}))
		r.Get("/", s.handleListUserTokens)
		r.Post("/", s.handleCreateUserToken)
		r.Route("/{tokenId}", func(r chi.Router) {
			r.Post("/rotate", s.handleRotateUserToken)
			r.Post("/revoke", s.handleRevokeUserToken)
		})
	})
}

func (s *Server) handleListUserTokens(w http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(chi.URLParam(r, "username"))
	if _, err := s.Auth.GetUser(username); err != nil {
		respondError(w, 404, "not_found", "user not found")
		return
	}
	tokens, err := s.Auth.ListTokensForUser(username)
	if err != nil {
		respondError(w, 500, "internal", "list failed")
		return
	}
	respondJSON(w, 200, map[string]any{"tokens": tokens})
}

func (s *Server) handleCreateUserToken(w http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(chi.URLParam(r, "username"))
	if _, err := s.Auth.GetUser(username); err != nil {
		respondError(w, 404, "not_found", "user not found")
		return
	}
	var req createTokenRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	caller := auth.UserFromContext(r.Context())
	createdBy := ""
	if caller != nil {
		createdBy = caller.Username
	}
	token, err := auth.GenerateToken()
	if err != nil {
		respondError(w, 500, "internal", "token gen failed")
		return
	}
	tok, err := s.Auth.CreateToken(auth.CreateTokenParams{
		Username:  username,
		TokenHash: auth.HashToken(token),
		Label:     req.Label,
		CreatedBy: createdBy,
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

func (s *Server) handleRotateUserToken(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleRevokeUserToken(w http.ResponseWriter, r *http.Request) {
	tokenID := chi.URLParam(r, "tokenId")
	callerToken := auth.TokenFromContext(r.Context())
	if callerToken != nil && callerToken.ID == tokenID {
		respondError(w, 400, "self_revoke_token",
			"cannot revoke the token you're using; rotate it instead, or create another token first")
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
