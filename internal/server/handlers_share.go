package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/christophermarx/agentboard/internal/share"
	"github.com/christophermarx/agentboard/internal/view"
)

// handleCreateShare issues a new share token for a page.
//
//	POST /api/share  body: { "path": "/handbook", "ttl_seconds": 604800 }
//
// Response contains the fragment-form URL (preferred) AND the token
// itself for the admin UI to copy. The SPA uses the fragment-URL form
// to redeem into a cookie on arrival.
func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	if s.Share == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "share store not available")
		return
	}
	var body struct {
		Path       string `json:"path"`
		TTLSeconds int64  `json:"ttl_seconds"`
		Label      string `json:"label,omitempty"`
		MaxUses    int    `json:"max_uses,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body must be JSON {path, ttl_seconds?, label?, max_uses?}")
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "path required")
		return
	}
	ttl := time.Duration(body.TTLSeconds) * time.Second
	if body.TTLSeconds == 0 {
		ttl = 7 * 24 * time.Hour
	}
	actor := resolveActor(r)
	if actor == "" || actor == "anonymous" {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "share tokens require an authenticated user")
		return
	}
	storedPath := "/" + strings.TrimPrefix(body.Path, "/")

	plaintext, tok, err := s.Share.Create(share.CreateParams{
		Path:      storedPath,
		CreatedBy: actor,
		TTL:       ttl,
		Label:     body.Label,
		MaxUses:   body.MaxUses,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	scheme := "http"
	if requestIsSecure(r) || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	// Fragment form — hidden from logs + referrer. Preferred.
	url := scheme + "://" + host + storedPath + "#share=" + plaintext

	resp := map[string]any{
		"id":         tok.ID,
		"token":      plaintext,
		"url":        url,
		"path":       tok.Path,
		"created_by": tok.CreatedBy,
		"created_at": tok.CreatedAt,
	}
	if tok.ExpiresAt != nil {
		resp["expires_at"] = *tok.ExpiresAt
	}
	respondJSON(w, http.StatusOK, resp)
}

// handleListShares lists unrevoked share tokens for a page.
//
//	GET /api/share?path=/handbook
func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	if s.Share == nil {
		respondJSON(w, http.StatusOK, []any{})
		return
	}
	pagePath := r.URL.Query().Get("path")
	if pagePath == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "?path= query param required")
		return
	}
	storedPath := "/" + strings.TrimPrefix(pagePath, "/")
	toks, err := s.Share.ListForPath(storedPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, shareListPayload(toks))
}

// handleAdminListShares returns every unrevoked share across the
// instance. Admin-only — scoped inside /api/admin/*.
func (s *Server) handleAdminListShares(w http.ResponseWriter, r *http.Request) {
	if s.Share == nil {
		respondJSON(w, http.StatusOK, []any{})
		return
	}
	toks, err := s.Share.ListAll()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, shareListPayload(toks))
}

// shareListPayload is the shared serialization for list responses.
func shareListPayload(toks []*share.Token) []map[string]any {
	out := make([]map[string]any, 0, len(toks))
	for _, t := range toks {
		m := map[string]any{
			"id":         t.ID,
			"path":       t.Path,
			"created_by": t.CreatedBy,
			"created_at": t.CreatedAt,
			"use_count":  t.UseCount,
		}
		if t.ExpiresAt != nil {
			m["expires_at"] = *t.ExpiresAt
			if time.Now().UTC().After(*t.ExpiresAt) {
				m["expired"] = true
			}
		}
		if t.LastUsedAt != nil {
			m["last_used_at"] = *t.LastUsedAt
		}
		if t.Label != "" {
			m["label"] = t.Label
		}
		if t.MaxUses != nil {
			m["max_uses"] = *t.MaxUses
		}
		out = append(out, m)
	}
	return out
}

// handleRevokeShare revokes a share token by ID and cascade-deletes
// any active view-sessions minted from it — closing open SSE streams.
func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	if s.Share == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "share store not available")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/share/")
	if id == "" {
		respondError(w, http.StatusBadRequest, "INVALID_ID", "share id required")
		return
	}
	if err := s.Share.Revoke(id); err != nil {
		if errors.Is(err, share.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "share token not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	// Cascade: drop any view-sessions backed by this token so the
	// cookie held by the visitor stops working immediately.
	if s.ViewSessions != nil {
		_ = s.ViewSessions.DeleteByShareToken(id)
	}
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRedeemShare exchanges a plaintext share token (pulled from the
// URL fragment by the SPA) for an HttpOnly cookie. The plaintext
// touches the wire exactly once — redeem — and never lands in server
// logs after this call because subsequent requests carry only the
// opaque cookie.
//
//	POST /api/share/redeem  body: { "token": "sh_..." }
//
// On success: Set-Cookie: ab_view_session=<opaque>; HttpOnly; SameSite=Strict
func (s *Server) handleRedeemShare(w http.ResponseWriter, r *http.Request) {
	if s.Share == nil || s.ViewSessions == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "share redeem unavailable")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body must be JSON {token}")
		return
	}
	if !strings.HasPrefix(body.Token, share.TokenPrefix) {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "expected sh_ token")
		return
	}
	tok, err := s.Share.Resolve(body.Token)
	if err != nil {
		switch {
		case errors.Is(err, share.ErrRevoked):
			respondError(w, http.StatusUnauthorized, "REVOKED", "share token revoked")
		case errors.Is(err, share.ErrExpired):
			respondError(w, http.StatusUnauthorized, "EXPIRED", "share token expired")
		case errors.Is(err, share.ErrNotFound):
			respondError(w, http.StatusUnauthorized, "NOT_FOUND", "share token not found")
		default:
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
		return
	}
	cookiePlain, sess, err := s.ViewSessions.Create(tok.ID, tok.Path, view.DefaultSessionTTL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     view.SessionCookieName,
		Value:    cookiePlain,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
	respondJSON(w, http.StatusOK, map[string]any{
		"path":       tok.Path,
		"expires_at": sess.ExpiresAt,
	})
}
