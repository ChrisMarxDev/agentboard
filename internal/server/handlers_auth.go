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

// /api/auth/* — browser-session login surface added on top of the
// existing token model. Tokens stay the canonical credential for
// non-human callers (CLI, MCP, agents); sessions are the credential
// for humans in a browser, minted via username + password.
//
// Everything here is additive. Bearer auth still works on every
// gated route exactly as before.

// loginRequest is the body of POST /api/auth/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// authResponse is the shape returned by /api/auth/login and
// /api/auth/me — the SPA reads `user` to populate the shell.
type authResponse struct {
	User publicUser `json:"user"`
}

// passwordChangeRequest is the body of /api/users/{u}/password.
//
// CurrentPassword is required when the caller is the user themselves.
// Admins setting another user's password may omit it (force-set).
type passwordChangeRequest struct {
	CurrentPassword string `json:"current_password,omitempty"`
	NewPassword     string `json:"new_password"`
}

// registerAuthRoutes wires the /api/auth/* surface. Mounted inside
// /api so it inherits the same rejectAPITraversal + apiNotFound
// envelopes as the rest of the API; the open-paths list in
// server.go marks /api/auth/login + /api/auth/me as anonymous-OK so
// the login round-trip can complete before a session exists.
func (s *Server) registerAuthRoutes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Post("/login", s.handleAuthLogin)
		r.Post("/logout", s.handleAuthLogout)
		r.Get("/me", s.handleAuthMe)
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		respondError(w, 503, "unavailable", "auth store unavailable")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))
	if username == "" || req.Password == "" {
		// Same shape as wrong-password to avoid leaking which field
		// was missing — there's no useful UX cost to merging these
		// branches.
		respondError(w, 401, "invalid_credentials", "username or password incorrect")
		return
	}

	user, err := s.Auth.VerifyLogin(username, req.Password)
	if err != nil {
		// Every failure surfaces the same status + body. Swallow the
		// specific error so timing differences are the only signal
		// (and those are flattened by VerifyLogin's dummy-hash
		// fallback).
		respondError(w, 401, "invalid_credentials", "username or password incorrect")
		return
	}

	_, plain, err := s.Auth.CreateSession(user.Username, r.UserAgent(), clientIP(r), 0)
	if err != nil {
		respondError(w, 500, "internal", "session create failed")
		return
	}
	csrf, err := auth.GenerateCSRFToken()
	if err != nil {
		respondError(w, 500, "internal", "csrf gen failed")
		return
	}
	setSessionCookies(w, plain, csrf, auth.DefaultSessionTTL, r.TLS != nil)

	pu := toPublic(user)
	respondJSON(w, 200, authResponse{User: pu})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	// Best-effort revoke: if the request is authenticated by a
	// session, mark it revoked. Bearer-authenticated logout calls
	// (an agent script that calls /api/auth/logout for some reason)
	// are no-ops on the server, but still clear the cookies for
	// good measure.
	if sess := auth.SessionFromContext(r.Context()); sess != nil {
		_ = s.Auth.RevokeSession(sess.ID)
	} else if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		// No middleware-resolved session, but there's a cookie. Try
		// to revoke whatever it points at so a stolen cookie value
		// can't outlive the user clicking "log out".
		if _, sess, err := s.Auth.ResolveSession(cookie.Value); err == nil && sess != nil {
			_ = s.Auth.RevokeSession(sess.ID)
		}
	}
	clearSessionCookies(w, r.TLS != nil)
	respondJSON(w, 200, map[string]string{"status": "logged_out"})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	// /api/auth/me is registered as an open path so a probe call
	// from the SPA's session boot doesn't trigger a 401-redirect
	// loop on /login. That means TokenMiddleware never resolved
	// the request, so do it inline. Two credential shapes work:
	// session cookie (the new path) and bearer token (so existing
	// PAT-based callers can still hit this endpoint).
	if user, ok := s.resolveCallerForAuthMe(r); ok {
		pu := toPublic(user)
		respondJSON(w, 200, authResponse{User: pu})
		return
	}
	respondError(w, 401, "unauthorized", "not signed in")
}

// resolveCallerForAuthMe is the inline auth resolver for
// /api/auth/me. Returns (user, true) when any of session cookie /
// bearer-PAT credentials authenticates the request.
func (s *Server) resolveCallerForAuthMe(r *http.Request) (*auth.User, bool) {
	if s.Auth == nil {
		return nil, false
	}
	// Session cookie path.
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		if user, _, err := s.Auth.ResolveSession(cookie.Value); err == nil {
			return user, true
		}
	}
	// Bearer-PAT path — keeps the legacy "validate this token by
	// hitting /api/auth/me" check working.
	if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
		token := strings.TrimPrefix(ah, "Bearer ")
		if !strings.HasPrefix(token, auth.OAuthAccessPrefix) && token != "" {
			if user, _, err := s.Auth.ResolveToken(auth.HashToken(token)); err == nil {
				return user, true
			}
		}
	}
	return nil, false
}

// -------------------- password change --------------------

// registerUserPasswordRoutes wires /api/users/{u}/password under the
// same self-or-admin scope as /api/users/{u}/tokens/*.
func (s *Server) registerUserPasswordRoutes(r chi.Router) {
	r.Route("/users/{username}/password", func(r chi.Router) {
		r.Use(auth.ScopeSelfOrAdmin(func(r *http.Request, name string) string {
			return chi.URLParam(r, name)
		}))
		r.Post("/", s.handleSetUserPassword)
		r.Delete("/", s.handleDeleteUserPassword)
	})
}

func (s *Server) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		respondError(w, 503, "unavailable", "auth store unavailable")
		return
	}
	username := strings.ToLower(chi.URLParam(r, "username"))
	target, err := s.Auth.GetUser(username)
	if err != nil {
		respondError(w, 404, "not_found", "user not found")
		return
	}

	var req passwordChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if req.NewPassword == "" {
		respondError(w, 400, "bad_request", "new_password required")
		return
	}

	caller := auth.UserFromContext(r.Context())
	isSelf := caller != nil && strings.EqualFold(caller.Username, target.Username)
	isAdmin := caller != nil && caller.Kind == auth.KindAdmin

	// A user changing their own password must prove they know the
	// current one — defense against stolen-session take-over of a
	// long-running login. Admins may force-set without knowing it.
	if isSelf && !isAdmin {
		if req.CurrentPassword == "" {
			respondError(w, 400, "bad_request", "current_password required")
			return
		}
		if _, err := s.Auth.VerifyLogin(target.Username, req.CurrentPassword); err != nil {
			respondError(w, 401, "invalid_credentials", "current password incorrect")
			return
		}
	}

	if err := s.Auth.SetPassword(target.Username, req.NewPassword); err != nil {
		if errors.Is(err, auth.ErrWeakPassword) {
			respondError(w, 400, "weak_password", err.Error())
			return
		}
		respondError(w, 500, "internal", "set password failed")
		return
	}

	respondJSON(w, 200, map[string]string{"status": "password_updated"})
}

// handleDeleteUserPassword clears the password on a user. Only
// reachable by self or admin. Sessions are NOT revoked here — call
// the dedicated session-revoke endpoint for that.
func (s *Server) handleDeleteUserPassword(w http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(chi.URLParam(r, "username"))
	if _, err := s.Auth.GetUser(username); err != nil {
		respondError(w, 404, "not_found", "user not found")
		return
	}
	if err := s.Auth.ClearPassword(username); err != nil {
		respondError(w, 500, "internal", "clear failed")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "password_cleared"})
}

// -------------------- sessions surface --------------------

// registerUserSessionRoutes adds the per-user session list +
// revoke surface under self-or-admin scope.
//
//	GET    /api/users/{u}/sessions             — list rows (active + revoked)
//	DELETE /api/users/{u}/sessions/{id}        — revoke one
//	POST   /api/users/{u}/sessions/revoke-all  — revoke every active session
func (s *Server) registerUserSessionRoutes(r chi.Router) {
	r.Route("/users/{username}/sessions", func(r chi.Router) {
		r.Use(auth.ScopeSelfOrAdmin(func(r *http.Request, name string) string {
			return chi.URLParam(r, name)
		}))
		r.Get("/", s.handleListUserSessions)
		r.Post("/revoke-all", s.handleRevokeAllUserSessions)
		r.Delete("/{id}", s.handleRevokeUserSession)
	})
}

// publicSession is what we hand back to clients. The session id is
// included so the UI can render a per-row "Revoke" button.
type publicSession struct {
	ID         string     `json:"id"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	UserAgent  string     `json:"user_agent,omitempty"`
	IP         string     `json:"ip,omitempty"`
	Current    bool       `json:"current,omitempty"`
}

func (s *Server) handleListUserSessions(w http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(chi.URLParam(r, "username"))
	if _, err := s.Auth.GetUser(username); err != nil {
		respondError(w, 404, "not_found", "user not found")
		return
	}
	rows, err := s.Auth.ListSessionsForUser(username)
	if err != nil {
		respondError(w, 500, "internal", "list failed")
		return
	}
	cur := auth.SessionFromContext(r.Context())
	out := make([]publicSession, 0, len(rows))
	for _, sess := range rows {
		ps := publicSession{
			ID:         sess.ID,
			CreatedAt:  sess.CreatedAt,
			LastUsedAt: sess.LastUsedAt,
			ExpiresAt:  sess.ExpiresAt,
			RevokedAt:  sess.RevokedAt,
			UserAgent:  sess.UserAgent,
			IP:         sess.IP,
		}
		if cur != nil && cur.ID == sess.ID {
			ps.Current = true
		}
		out = append(out, ps)
	}
	respondJSON(w, 200, map[string]any{"sessions": out})
}

func (s *Server) handleRevokeUserSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.Auth.RevokeSession(id); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			respondError(w, 404, "not_found", "session not found or already revoked")
			return
		}
		respondError(w, 500, "internal", "revoke failed")
		return
	}
	respondJSON(w, 200, map[string]string{"status": "revoked"})
}

func (s *Server) handleRevokeAllUserSessions(w http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(chi.URLParam(r, "username"))
	n, err := s.Auth.RevokeAllSessionsForUser(username)
	if err != nil {
		respondError(w, 500, "internal", "revoke-all failed")
		return
	}
	respondJSON(w, 200, map[string]any{"status": "revoked", "count": n})
}

// -------------------- cookie helpers --------------------

// setSessionCookies emits the agentboard_session (HttpOnly) +
// agentboard_csrf (readable by JS) cookies. Path=/ so every API call
// from the SPA carries them; Secure when the request was TLS.
func setSessionCookies(w http.ResponseWriter, sessionPlain, csrf string, ttl time.Duration, secure bool) {
	expires := time.Now().UTC().Add(ttl)
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    sessionPlain,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CSRFCookieName,
		Value:    csrf,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: false, // the SPA reads it via document.cookie
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookies overwrites the two cookies with empty values
// + immediate expiry, which is how http.Cookie.MaxAge=-1 deletes
// them in browser implementations.
func clearSessionCookies(w http.ResponseWriter, secure bool) {
	for _, name := range []string{auth.SessionCookieName, auth.CSRFCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			Expires:  time.Unix(0, 0),
			MaxAge:   -1,
			HttpOnly: name == auth.SessionCookieName,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// clientIP picks a best-guess remote address. We trust X-Forwarded-For
// when it's set (we run behind Cloudflare / Coolify in production)
// but fall back to RemoteAddr for direct connections (dev). This is
// purely informational — never used for an authn/authz decision.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First entry is the originating client.
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	return r.RemoteAddr
}
