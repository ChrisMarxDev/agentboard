package server

import (
	"fmt"
	"net/http"
)

// /api/setup/status is the "is this board claimed yet?" hint the SPA
// uses to decide what /login should show. It's the only setup-adjacent
// endpoint that remains — browser-claim via POST /api/setup was removed
// in Auth v1 in favor of the init-mints-invite flow.
//
// When the board is unclaimed AND an active bootstrap invitation exists,
// the response includes its redeem URL so the frontend can deep-link
// the operator to /invite/<id> without them having to fish the URL out
// of the server log.

type setupStatusResponse struct {
	Initialized bool   `json:"initialized"`
	InviteURL   string `json:"invite_url,omitempty"`
}

// handleSetupStatus answers "has this board been claimed yet?" Open
// (no token required) because it gates the /login UI's form choice.
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	has, err := s.Auth.HasAnyUser()
	if err != nil {
		respondError(w, 500, "internal", "status check failed")
		return
	}
	resp := setupStatusResponse{Initialized: has}
	if !has && s.Invitations != nil {
		if inv, err := s.Invitations.BootstrapActive(); err == nil && inv != nil {
			// Build a same-origin URL. The scheme is derived from the
			// request — since this endpoint is often hit over HTTPS in
			// production via a reverse proxy, honour X-Forwarded-Proto
			// when present.
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
				scheme = proto
			}
			resp.InviteURL = fmt.Sprintf("%s://%s/invite/%s", scheme, r.Host, inv.ID)
		}
	}
	respondJSON(w, 200, resp)
}
