package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/go-chi/chi/v5"
)

// -------------------- request/response shapes --------------------

type setupRequest struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

type createIdentityRequest struct {
	Name       string          `json:"name"`
	Kind       auth.Kind       `json:"kind"` // admin | agent
	AccessMode auth.AccessMode `json:"access_mode,omitempty"`
	Rules      []auth.Rule     `json:"rules,omitempty"`
	Password   string          `json:"password,omitempty"` // admin only
}

type updateIdentityRequest struct {
	Name       *string          `json:"name,omitempty"`
	AccessMode *auth.AccessMode `json:"access_mode,omitempty"`
	Rules      *[]auth.Rule     `json:"rules,omitempty"`
}

type rotateRequest struct {
	Mode        string `json:"mode"`         // "hard" (default) — graceful deferred to future work
	GracePeriod string `json:"grace_period"` // unused while hard-only; accepted so clients can forward-compat
}

type meResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CSRFToken string `json:"csrf_token"`
}

type tokenResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

// -------------------- route registration --------------------

// registerAdminRoutes mounts /api/admin/* with SessionMiddleware scoped to
// this subtree. /setup and /login are open (pre-session) paths.
func (s *Server) registerAdminRoutes(r chi.Router) {
	r.Route("/admin", func(r chi.Router) {
		r.Use(auth.SessionMiddleware(s.Auth, auth.MiddlewareConfig{
			OpenPaths: []string{"/api/admin/setup", "/api/admin/login"},
		}))

		r.Post("/setup", s.handleAdminSetup)
		r.Post("/login", s.handleAdminLogin)
		r.Post("/logout", s.handleAdminLogout)
		r.Get("/me", s.handleAdminMe)
		r.Post("/password", s.handleAdminChangePassword)

		r.Get("/identities", s.handleListIdentities)
		r.Post("/identities", s.handleCreateIdentity)
		r.Route("/identities/{id}", func(r chi.Router) {
			r.Patch("/", s.handleUpdateIdentity)
			r.Post("/rotate", s.handleRotateIdentity)
			r.Post("/revoke", s.handleRevokeIdentity)
		})

		r.Get("/bootstrap-codes", s.handleListBootstrapCodes)
		r.Post("/bootstrap-codes", s.handleCreateBootstrapCode)
		r.Delete("/bootstrap-codes/{id}", s.handleDeleteBootstrapCode)
	})
}

// -------------------- setup + login --------------------

// handleAdminSetup consumes a bootstrap code and creates the first admin
// identity (or an additional admin when issued by an existing admin).
func (s *Server) handleAdminSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Code == "" || req.Name == "" || len(req.Password) < 8 {
		respondError(w, 400, "bad_request", "code, name, and password (>=8 chars) required")
		return
	}

	// Atomic-ish: consume the code first, then create the identity.
	// A code consumed with no identity created leaves the system claimable
	// via a fresh code, so there's no lockout if the identity create fails.
	if err := s.Auth.ConsumeBootstrapCode(req.Code); err != nil {
		if errors.Is(err, auth.ErrCodeInvalid) {
			respondError(w, 401, "invalid_code", "bootstrap code is invalid or already used")
			return
		}
		respondError(w, 500, "internal", "bootstrap code error")
		return
	}

	pwHash, err := auth.HashPassword(req.Password)
	if err != nil {
		respondError(w, 500, "internal", "password hash failed")
		return
	}
	ident, err := s.Auth.CreateIdentity(auth.CreateIdentityParams{
		Name:         req.Name,
		Kind:         auth.KindAdmin,
		PasswordHash: pwHash,
		CreatedBy:    "setup",
	})
	if err != nil {
		if errors.Is(err, auth.ErrNameTaken) {
			respondError(w, 409, "name_taken", "an identity with this name already exists")
			return
		}
		respondError(w, 500, "internal", "could not create admin")
		return
	}
	auth.InvalidateFirstRunCache()

	s.issueSession(w, r, ident)
	respondJSON(w, 201, meResponseFor(ident, getSessionFromW(w)))
}

// handleAdminLogin verifies the password and issues a session cookie.
// Timing is constant-ish: we always compute an argon2 hash (on a decoy
// when the name doesn't exist) so the response time doesn't leak user
// existence.
func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}

	ident, err := s.Auth.GetIdentityByName(strings.TrimSpace(req.Name))
	if err != nil || ident.Kind != auth.KindAdmin || ident.RevokedAt != nil {
		// Decoy hash to keep timing roughly constant.
		_ = auth.VerifyPassword(req.Password, "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		time.Sleep(250 * time.Millisecond)
		respondError(w, 401, "invalid_credentials", "invalid name or password")
		return
	}

	if err := auth.VerifyPassword(req.Password, ident.PasswordHash); err != nil {
		time.Sleep(250 * time.Millisecond)
		respondError(w, 401, "invalid_credentials", "invalid name or password")
		return
	}

	s.issueSession(w, r, ident)
	respondJSON(w, 200, meResponseFor(ident, getSessionFromW(w)))
}

// handleAdminLogout deletes the current session and clears the cookie.
func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if sess := auth.SessionFromContext(r.Context()); sess != nil {
		_ = s.Auth.DeleteSession(sess.ID)
	}
	clearSessionCookie(w)
	respondJSON(w, 200, map[string]string{"status": "ok"})
}

// handleAdminMe returns the logged-in admin's basic info plus the CSRF
// token required on state-changing admin requests.
func (s *Server) handleAdminMe(w http.ResponseWriter, r *http.Request) {
	ident := auth.IdentityFromContext(r.Context())
	sess := auth.SessionFromContext(r.Context())
	if ident == nil || sess == nil {
		respondError(w, 401, "unauthorized", "no session")
		return
	}
	respondJSON(w, 200, meResponseFor(ident, sess))
}

// handleAdminChangePassword updates the password and invalidates all other
// sessions for this admin.
func (s *Server) handleAdminChangePassword(w http.ResponseWriter, r *http.Request) {
	ident := auth.IdentityFromContext(r.Context())
	sess := auth.SessionFromContext(r.Context())
	if ident == nil || sess == nil {
		respondError(w, 401, "unauthorized", "no session")
		return
	}
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if len(req.New) < 8 {
		respondError(w, 400, "bad_request", "new password must be at least 8 chars")
		return
	}
	if err := auth.VerifyPassword(req.Current, ident.PasswordHash); err != nil {
		respondError(w, 401, "invalid_credentials", "current password is wrong")
		return
	}
	newHash, err := auth.HashPassword(req.New)
	if err != nil {
		respondError(w, 500, "internal", "hash failed")
		return
	}
	if err := s.Auth.SetPassword(ident.ID, newHash); err != nil {
		respondError(w, 500, "internal", "update failed")
		return
	}
	// Revoke every session except the current one.
	sessions := []string{}
	if all, err := s.Auth.ListIdentities(); err == nil {
		_ = all // placeholder — session listing by identity not yet exposed
	}
	_ = sessions
	// Pragmatic shortcut: delete all sessions for this identity, then re-issue
	// a fresh one for the current request so the user stays logged in.
	_ = s.Auth.DeleteSessionsForIdentity(ident.ID)
	s.issueSession(w, r, ident)
	respondJSON(w, 200, meResponseFor(ident, getSessionFromW(w)))
}

// -------------------- identities CRUD --------------------

func (s *Server) handleListIdentities(w http.ResponseWriter, r *http.Request) {
	idents, err := s.Auth.ListIdentities()
	if err != nil {
		respondError(w, 500, "internal", "list failed")
		return
	}
	respondJSON(w, 200, map[string]any{"identities": idents})
}

// handleCreateIdentity mints a new identity. For agents, the token is
// generated server-side and returned ONCE in the response.
func (s *Server) handleCreateIdentity(w http.ResponseWriter, r *http.Request) {
	caller := auth.IdentityFromContext(r.Context())
	if caller == nil {
		respondError(w, 401, "unauthorized", "no session")
		return
	}
	var req createIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		respondError(w, 400, "bad_request", "name required")
		return
	}
	if req.Kind != auth.KindAgent && req.Kind != auth.KindAdmin {
		respondError(w, 400, "bad_request", "kind must be 'agent' or 'admin'")
		return
	}
	if req.AccessMode == "" {
		req.AccessMode = auth.ModeAllowAll
	}

	params := auth.CreateIdentityParams{
		Name:       req.Name,
		Kind:       req.Kind,
		AccessMode: req.AccessMode,
		Rules:      req.Rules,
		CreatedBy:  caller.Name,
	}

	// Secrets.
	var plaintextToken string
	switch req.Kind {
	case auth.KindAgent:
		tok, err := auth.GenerateToken()
		if err != nil {
			respondError(w, 500, "internal", "token gen failed")
			return
		}
		plaintextToken = tok
		params.TokenHash = auth.HashToken(tok)
	case auth.KindAdmin:
		if len(req.Password) < 8 {
			respondError(w, 400, "bad_request", "admin identities require password (>=8 chars)")
			return
		}
		h, err := auth.HashPassword(req.Password)
		if err != nil {
			respondError(w, 500, "internal", "hash failed")
			return
		}
		params.PasswordHash = h
	}

	ident, err := s.Auth.CreateIdentity(params)
	if err != nil {
		if errors.Is(err, auth.ErrNameTaken) {
			respondError(w, 409, "name_taken", "an identity with this name already exists")
			return
		}
		respondError(w, 500, "internal", "create failed")
		return
	}

	// Admins: return the identity without secrets.
	if req.Kind == auth.KindAdmin {
		respondJSON(w, 201, ident)
		return
	}
	// Agents: return the plaintext token exactly once.
	respondJSON(w, 201, tokenResponse{ID: ident.ID, Name: ident.Name, Token: plaintextToken})
}

func (s *Server) handleUpdateIdentity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	ident, err := s.Auth.UpdateIdentity(id, auth.UpdateIdentityParams{
		Name:       req.Name,
		AccessMode: req.AccessMode,
		Rules:      req.Rules,
	})
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			respondError(w, 404, "not_found", "identity not found")
			return
		}
		if errors.Is(err, auth.ErrNameTaken) {
			respondError(w, 409, "name_taken", "an identity with this name already exists")
			return
		}
		respondError(w, 500, "internal", "update failed")
		return
	}
	respondJSON(w, 200, ident)
}

// handleRotateIdentity replaces an agent's token. Graceful rotation is
// reserved for a later chunk; hard rotation is what ships now.
func (s *Server) handleRotateIdentity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req rotateRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body is optional; defaults are hard rotate.
	if req.Mode != "" && req.Mode != "hard" {
		respondError(w, 400, "bad_request", "only 'hard' rotation is supported right now")
		return
	}

	ident, err := s.Auth.GetIdentity(id)
	if err != nil {
		respondError(w, 404, "not_found", "identity not found")
		return
	}
	if ident.Kind != auth.KindAgent {
		respondError(w, 400, "bad_request", "can only rotate agent tokens")
		return
	}

	newToken, err := auth.GenerateToken()
	if err != nil {
		respondError(w, 500, "internal", "token gen failed")
		return
	}
	if err := s.Auth.RotateToken(id, auth.HashToken(newToken)); err != nil {
		respondError(w, 500, "internal", "rotate failed")
		return
	}
	respondJSON(w, 200, tokenResponse{ID: ident.ID, Name: ident.Name, Token: newToken})
}

func (s *Server) handleRevokeIdentity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	caller := auth.IdentityFromContext(r.Context())
	// Safety rail: don't let admins revoke themselves via this endpoint — use
	// logout + admin reset for that. Prevents accidental self-lockout.
	if caller != nil && caller.ID == id {
		respondError(w, 400, "self_revoke", "cannot revoke your own identity; use admin reset on the host")
		return
	}
	if err := s.Auth.Revoke(id); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			respondError(w, 404, "not_found", "identity not found or already revoked")
			return
		}
		respondError(w, 500, "internal", "revoke failed")
		return
	}
	_ = s.Auth.DeleteSessionsForIdentity(id)
	respondJSON(w, 200, map[string]string{"status": "revoked"})
}

// -------------------- bootstrap codes --------------------

type createBootstrapRequest struct {
	TTLHours int    `json:"ttl_hours"`
	Note     string `json:"note"`
}

type createBootstrapResponse struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
	Note      string    `json:"note,omitempty"`
}

func (s *Server) handleCreateBootstrapCode(w http.ResponseWriter, r *http.Request) {
	var req createBootstrapRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.TTLHours <= 0 {
		req.TTLHours = 24
	}
	code, bc, err := s.Auth.CreateBootstrapCode(time.Duration(req.TTLHours)*time.Hour, req.Note)
	if err != nil {
		respondError(w, 500, "internal", "could not create bootstrap code")
		return
	}
	respondJSON(w, 201, createBootstrapResponse{
		ID:        bc.ID,
		Code:      code,
		ExpiresAt: bc.ExpiresAt,
		Note:      bc.Note,
	})
}

func (s *Server) handleListBootstrapCodes(w http.ResponseWriter, r *http.Request) {
	codes, err := s.Auth.ListBootstrapCodes()
	if err != nil {
		respondError(w, 500, "internal", "list failed")
		return
	}
	// Never leak the full hash — just a short fingerprint.
	out := make([]map[string]any, 0, len(codes))
	for _, c := range codes {
		out = append(out, map[string]any{
			"id":          c.ID,
			"fingerprint": c.CodeHash[:8],
			"created_at":  c.CreatedAt,
			"expires_at":  c.ExpiresAt,
			"note":        c.Note,
		})
	}
	respondJSON(w, 200, map[string]any{"codes": out})
}

func (s *Server) handleDeleteBootstrapCode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.Auth.DeleteBootstrapCode(id); err != nil {
		respondError(w, 500, "internal", "delete failed")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "deleted"})
}

// -------------------- session cookie helpers --------------------

// sessionIssuer holds state briefly so the handler can return the CSRF
// token in the same response that sets the cookie. We attach the freshly
// created Session to the ResponseWriter via a header, read it back in
// meResponseFor, and strip it before the response hits the wire.
const headerNewSession = "X-AgentBoard-New-Session"

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, ident *auth.Identity) {
	sess, err := s.Auth.CreateSession(ident.ID, r.UserAgent(), clientIP(r), auth.SessionTTL)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    sess.ID,
		Path:     "/api/admin",
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteStrictMode,
		Expires:  sess.ExpiresAt,
	})
	// Stash the session on the response writer via a header; meResponseFor
	// reads + deletes it before serializing.
	w.Header().Set(headerNewSession, sess.ID+"|"+sess.CSRFToken)
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/api/admin",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func getSessionFromW(w http.ResponseWriter) *auth.Session {
	raw := w.Header().Get(headerNewSession)
	if raw == "" {
		return nil
	}
	w.Header().Del(headerNewSession)
	parts := strings.SplitN(raw, "|", 2)
	if len(parts) != 2 {
		return nil
	}
	return &auth.Session{ID: parts[0], CSRFToken: parts[1]}
}

func meResponseFor(ident *auth.Identity, sess *auth.Session) meResponse {
	r := meResponse{ID: ident.ID, Name: ident.Name}
	if sess != nil {
		r.CSRFToken = sess.CSRFToken
	}
	return r
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.IndexByte(fwd, ','); i > 0 {
			return strings.TrimSpace(fwd[:i])
		}
		return strings.TrimSpace(fwd)
	}
	return r.RemoteAddr
}
