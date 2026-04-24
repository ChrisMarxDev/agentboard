package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
)

// Setup = the one-time "claim this board" flow. Only valid while no user
// has been created yet; after that, this endpoint permanently 409s.
//
// See AUTH.md §"Bootstrap + recovery" for why this moved from
// "installer prints a token" to "first browser visitor claims admin".

type setupStatusResponse struct {
	Initialized bool `json:"initialized"`
}

// handleSetupStatus answers "has this board been claimed yet?" It's open
// (no token required) because it gates the /login UI's form choice.
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	has, err := s.Auth.HasAnyUser()
	if err != nil {
		respondError(w, 500, "internal", "status check failed")
		return
	}
	respondJSON(w, 200, setupStatusResponse{Initialized: has})
}

type setupRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
}

type setupResponse struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

// handleSetup creates the initial admin user + token. Guarded by the "no
// user exists yet" precondition — once any user exists this endpoint
// returns 409 forever.
//
// Races are bounded: if two clients try to claim simultaneously and both
// pass the HasAnyUser check, the second insert is still safe because
// usernames are the PK. Whoever lands first wins. Operators who care
// about the theoretical race window should bind to loopback and use
// `agentboard admin mint-admin` via the CLI instead.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	has, err := s.Auth.HasAnyUser()
	if err != nil {
		respondError(w, 500, "internal", "status check failed")
		return
	}
	if has {
		respondError(w, 409, "already_initialized", "this board has already been claimed")
		return
	}

	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))

	user, err := s.Auth.CreateUser(auth.CreateUserParams{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Kind:        auth.KindAdmin,
		CreatedBy:   "setup",
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidUsername):
			respondError(w, 400, "invalid_username", err.Error())
		case errors.Is(err, auth.ErrUsernameTaken):
			// Either a simultaneous race winner beat us, or (harmless)
			// the previous setup call created this username before we
			// re-checked. Either way the board is now claimed.
			respondError(w, 409, "already_initialized", "this board was just claimed by someone else")
		default:
			respondError(w, 500, "internal", "create failed")
		}
		return
	}
	token, err := auth.GenerateToken()
	if err != nil {
		respondError(w, 500, "internal", "token gen failed")
		return
	}
	if _, err := s.Auth.CreateToken(auth.CreateTokenParams{
		Username:  user.Username,
		TokenHash: auth.HashToken(token),
		Label:     "initial",
	}); err != nil {
		respondError(w, 500, "internal", "token persist failed")
		return
	}
	respondJSON(w, 201, setupResponse{Username: user.Username, Token: token})
}
