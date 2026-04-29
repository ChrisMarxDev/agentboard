package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/invitations"
	"github.com/go-chi/chi/v5"
)

// Invitation surface:
//
//	GET    /api/invitations/{id}            — public; used by /invite/<id> page
//	POST   /api/invitations/{id}/redeem     — public; mints user + token
//	POST   /api/admin/invitations           — admin; create new invite
//	GET    /api/admin/invitations           — admin; list all invites
//	DELETE /api/admin/invitations/{id}      — admin; revoke
//
// Redemption is the only HTTP path that can create a user from an
// anonymous caller. The server MUST do everything — validate the
// invite, create the user, mint the first token — in one atomic step,
// because a user row without a bootstrap token is useless.

// -------- public redeem --------

// publicInvitationView is what /api/invitations/{id} returns to an
// unauthenticated caller. Restricted fields only — no token IDs, no
// redeem-history, no revocation timestamps. Enough to render "Alice
// invited you as a member, expires in 6 days."
type publicInvitationView struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	CreatedBy string    `json:"created_by"`
	Label     string    `json:"label,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	Bootstrap bool      `json:"bootstrap"` // true if first-admin invite
}

func (s *Server) handleGetInvitationPublic(w http.ResponseWriter, r *http.Request) {
	if s.Invitations == nil {
		respondError(w, 503, "unavailable", "invitations store unavailable")
		return
	}
	id := chi.URLParam(r, "id")
	inv, err := s.Invitations.Get(id)
	if errors.Is(err, invitations.ErrNotFound) {
		respondError(w, 404, "not_found", "invitation not found")
		return
	}
	if err != nil {
		respondError(w, 500, "internal", "lookup failed")
		return
	}
	// Hide unusable invites behind 404 so the endpoint doesn't leak
	// whether an id ever existed.
	if inv.Status() != "active" {
		respondError(w, 404, "not_found", "invitation not found")
		return
	}
	respondJSON(w, 200, publicInvitationView{
		ID:        inv.ID,
		Role:      string(inv.Role),
		CreatedBy: inv.CreatedBy,
		Label:     inv.Label,
		ExpiresAt: inv.ExpiresAt,
		Bootstrap: inv.CreatedBy == invitations.BootstrapCreator,
	})
}

type redeemRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	// Password is optional. When supplied, the redeem flow sets it
	// on the user (argon2id-hashed) and mints a browser session,
	// returning the cookie alongside the token. When omitted, only
	// a token is minted — preserving the original v0 behaviour for
	// scripted/curl-driven redeems.
	Password string `json:"password,omitempty"`
}

type redeemResponse struct {
	Token        string        `json:"token"`
	User         *publicUser   `json:"user"`
	InvitationID string        `json:"invitation_id"`
	TokenID      string        `json:"token_id"`
	Role         string        `json:"role"`
	ExpiresAt    time.Time     `json:"expires_at,omitempty"`
}

// handleRedeemInvitation consumes the invitation + creates the user
// and first token in the same logical step. Concurrency window is
// handled by invitations.Redeem's atomic UPDATE.
//
// Failure modes (all idempotent on retry with a different username):
//   - invitation unusable (expired/redeemed/revoked) → 410 Gone
//   - invitation not found                            → 404
//   - username invalid                                → 400
//   - username taken                                  → 409 (invite NOT consumed)
//   - other server errors                             → 500
//
// Token is returned once — the caller stores it, then the browser
// redirects into the authenticated shell.
func (s *Server) handleRedeemInvitation(w http.ResponseWriter, r *http.Request) {
	if s.Invitations == nil || s.Auth == nil {
		respondError(w, 503, "unavailable", "invitations or auth store unavailable")
		return
	}
	id := chi.URLParam(r, "id")
	var req redeemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if req.Username == "" {
		respondError(w, 400, "bad_request", "username required")
		return
	}

	// Peek at the invite first. If the target username already exists,
	// refuse WITHOUT consuming the invite — the caller retries with a
	// different name.
	inv, err := s.Invitations.Get(id)
	if errors.Is(err, invitations.ErrNotFound) {
		respondError(w, 404, "not_found", "invitation not found")
		return
	}
	if err != nil {
		respondError(w, 500, "internal", "lookup failed")
		return
	}
	if existing, err := s.Auth.GetUser(req.Username); err == nil && existing != nil {
		respondError(w, 409, "username_taken", "username already in use — pick another")
		return
	} else if err != nil && !errors.Is(err, auth.ErrNotFound) {
		respondError(w, 500, "internal", "username check failed")
		return
	}

	// Claim the invite first. On any precondition miss, Redeem returns
	// a specific typed error we map below.
	redeemed, err := s.Invitations.Redeem(id, req.Username)
	if err != nil {
		switch {
		case errors.Is(err, invitations.ErrExpired):
			respondError(w, 410, "expired", "invitation expired")
		case errors.Is(err, invitations.ErrAlreadyRedeemed):
			respondError(w, 410, "already_redeemed", "invitation already used")
		case errors.Is(err, invitations.ErrRevoked):
			respondError(w, 410, "revoked", "invitation revoked")
		case errors.Is(err, invitations.ErrNotFound):
			respondError(w, 404, "not_found", "invitation not found")
		default:
			respondError(w, 500, "internal", "redeem failed")
		}
		return
	}

	// Create the user with the invite's role.
	user, err := s.Auth.CreateUser(auth.CreateUserParams{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Kind:        auth.Kind(redeemed.Role),
		CreatedBy:   "invitation:" + inv.ID,
	})
	if err != nil {
		// If we got here, the invite IS consumed. Best we can do is
		// report the failure; admin cleanup required.
		switch {
		case errors.Is(err, auth.ErrInvalidUsername):
			respondError(w, 400, "invalid_username", err.Error())
		case errors.Is(err, auth.ErrUsernameTaken):
			// Lost a race after the Get/Redeem. Rare. Surface as 409.
			respondError(w, 409, "username_taken", "username was just claimed")
		default:
			respondError(w, 500, "internal", "user create failed after redeem")
		}
		return
	}

	// Mint the first token. Tokens stay the credential of choice
	// for non-browser callers (CLI, MCP), so we always emit one,
	// even when the redeemer also picked a password.
	token, err := auth.GenerateToken()
	if err != nil {
		respondError(w, 500, "internal", "token gen failed")
		return
	}
	tok, err := s.Auth.CreateToken(auth.CreateTokenParams{
		Username:  user.Username,
		TokenHash: auth.HashToken(token),
		Label:     "initial",
		CreatedBy: "invitation:" + inv.ID,
	})
	if err != nil {
		respondError(w, 500, "internal", "token persist failed")
		return
	}

	// If the redeemer set a password, store the argon2id hash and
	// hand out a browser session in the same response so they're
	// signed in to the SPA without an extra round-trip.
	if req.Password != "" {
		if err := s.Auth.SetPassword(user.Username, req.Password); err != nil {
			if errors.Is(err, auth.ErrWeakPassword) {
				respondError(w, 400, "weak_password", err.Error())
				return
			}
			respondError(w, 500, "internal", "password persist failed")
			return
		}
		_, plainSession, err := s.Auth.CreateSession(user.Username, r.UserAgent(), clientIP(r), 0)
		if err == nil {
			if csrf, err := auth.GenerateCSRFToken(); err == nil {
				setSessionCookies(w, plainSession, csrf, auth.DefaultSessionTTL, r.TLS != nil)
			}
		}
	}

	pu := toPublic(user)
	respondJSON(w, 201, redeemResponse{
		Token:        token,
		TokenID:      tok.ID,
		User:         &pu,
		InvitationID: inv.ID,
		Role:         string(redeemed.Role),
	})
}

// -------- admin CRUD --------

type createInvitationRequest struct {
	Role          invitations.Role `json:"role"`
	Label         string           `json:"label,omitempty"`
	ExpiresInDays int              `json:"expires_in_days,omitempty"`
}

// invitationWithStatus inlines the derived status field so the admin
// UI doesn't have to recompute it client-side.
type invitationWithStatus struct {
	*invitations.Invitation
	Status string `json:"status"`
}

func (s *Server) registerAdminInvitationRoutes(r chi.Router) {
	r.Route("/invitations", func(r chi.Router) {
		r.Post("/", s.handleCreateInvitation)
		r.Get("/", s.handleListInvitations)
		r.Delete("/{id}", s.handleRevokeInvitation)
	})
}

func (s *Server) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	if s.Invitations == nil {
		respondError(w, 503, "unavailable", "invitations store unavailable")
		return
	}
	var req createInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if !invitations.ValidRole(req.Role) {
		respondError(w, 400, "bad_role", "role must be admin, member, or bot")
		return
	}
	expiresIn := time.Duration(req.ExpiresInDays) * 24 * time.Hour
	if expiresIn <= 0 {
		expiresIn = 7 * 24 * time.Hour
	}
	actor := ""
	if caller := auth.UserFromContext(r.Context()); caller != nil {
		actor = caller.Username
	}
	inv, err := s.Invitations.Create(invitations.CreateParams{
		Role:      req.Role,
		CreatedBy: actor,
		ExpiresIn: expiresIn,
		Label:     req.Label,
	})
	if err != nil {
		if errors.Is(err, invitations.ErrInvalidRole) {
			respondError(w, 400, "bad_role", err.Error())
			return
		}
		respondError(w, 500, "internal", "create failed")
		return
	}
	respondJSON(w, 201, invitationWithStatus{Invitation: inv, Status: inv.Status()})
}

func (s *Server) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	if s.Invitations == nil {
		respondJSON(w, 200, map[string]any{"invitations": []any{}})
		return
	}
	// Admin view shows everything, so they can see what was consumed
	// and by whom.
	list, err := s.Invitations.List(true)
	if err != nil {
		respondError(w, 500, "internal", "list failed")
		return
	}
	out := make([]invitationWithStatus, 0, len(list))
	for _, inv := range list {
		out = append(out, invitationWithStatus{Invitation: inv, Status: inv.Status()})
	}
	respondJSON(w, 200, map[string]any{"invitations": out})
}

func (s *Server) handleRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	if s.Invitations == nil {
		respondError(w, 503, "unavailable", "invitations store unavailable")
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.Invitations.Revoke(id); err != nil {
		if errors.Is(err, invitations.ErrNotFound) {
			respondError(w, 404, "not_found", "invitation not found")
			return
		}
		respondError(w, 500, "internal", "revoke failed")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "revoked"})
}
