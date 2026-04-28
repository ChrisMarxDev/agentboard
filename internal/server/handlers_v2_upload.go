package server

// Handlers for the presigned-URL file upload flow (spec §12).
// Two-step:
//   1) POST /api/v2/files/request-upload — auth required, mints token
//   2) PUT  /api/v2/upload/{token}       — no auth; bytes flow direct
//
// The second route is mounted OUTSIDE the bearer-required group; its
// authority is the unguessable token plus the size cap baked into the
// minted record. Tokens are one-shot — Redeem() removes the entry on
// the first lookup, so a leaked URL becomes inert after a single use
// (or 5 min, whichever comes first).

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/files"
	"github.com/go-chi/chi/v5"
)

// requestUploadBody is the input to /api/v2/files/request-upload.
type requestUploadBody struct {
	Name         string `json:"name"`
	SizeBytes    int64  `json:"size_bytes"`
	ContentType  string `json:"content_type,omitempty"` // advisory, server still sniffs
}

// requestUploadResponse mirrors what the spec described in §12. Agents
// use `upload_url` directly with `curl -X PUT --data-binary @file`.
type requestUploadResponse struct {
	UploadURL    string `json:"upload_url"`
	ExpiresAt    string `json:"expires_at"`
	MaxSizeBytes int64  `json:"max_size_bytes"`
	Token        string `json:"token"`
}

func (s *Server) handleV2RequestFileUpload(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil || s.UploadTokens == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "upload subsystem not configured")
		return
	}

	var body requestUploadBody
	if err := readJSONBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "name required")
		return
	}
	cleanName, err := files.ValidateName(body.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_name", err.Error())
		return
	}
	maxBytes := s.Files.MaxSizeBytes()
	if body.SizeBytes > maxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{
			Error:   "value_too_large",
			Message: fmt.Sprintf("file would exceed cap (got %d bytes, max %d)", body.SizeBytes, maxBytes),
		})
		return
	}

	tok := s.UploadTokens.Mint(cleanName, actorFor(r), maxBytes)

	// Build a self-contained URL that includes the host the caller can
	// reach. We don't attempt to be clever about reverse proxies —
	// the agent already knows where the server lives because it just
	// spoke to it; reflect the same scheme + host back.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}
	uploadURL := fmt.Sprintf("%s://%s/api/v2/upload/%s", scheme, host, tok.Token)

	writeJSON(w, http.StatusOK, requestUploadResponse{
		UploadURL:    uploadURL,
		ExpiresAt:    tok.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		MaxSizeBytes: tok.MaxSizeBytes,
		Token:        tok.Token,
	})
}

// handleV2UploadWithToken accepts the actual bytes. Mounted on the
// public router (no bearer middleware) — the token is the credential.
// Validates: token is live + matches the file name + size is within
// the cap. On any failure the bytes are dropped and a structured
// poka-yoke error returns.
func (s *Server) handleV2UploadWithToken(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil || s.UploadTokens == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "upload subsystem not configured")
		return
	}
	token := chi.URLParam(r, "token")
	if !strings.HasPrefix(token, "ut_") {
		writeError(w, http.StatusUnauthorized, "invalid_token", "upload token required")
		return
	}
	ut := s.UploadTokens.Redeem(token)
	if ut == nil {
		writeError(w, http.StatusUnauthorized, "expired_or_used",
			"upload token is invalid, expired, or already redeemed; request a fresh one")
		return
	}

	// Cap the body. Add 1 byte so we can reliably surface "too large".
	r.Body = http.MaxBytesReader(w, r.Body, ut.MaxSizeBytes+1)

	info, err := s.Files.Write(ut.Name, r.Body)
	switch {
	case errors.Is(err, files.ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_name", "file name no longer valid")
		return
	case errors.Is(err, files.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "value_too_large", "file exceeds size cap")
		return
	case err != nil:
		if strings.Contains(err.Error(), "too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "value_too_large", "file exceeds size cap")
			return
		}
		writeError(w, http.StatusInternalServerError, "write_error", err.Error())
		return
	}

	s.Broadcaster.Broadcast(SSEEvent{
		Type: "file-updated",
		Data: []byte(`{"name":"` + info.Name + `","deleted":false}`),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"name":         info.Name,
		"size":         info.Size,
		"content_type": info.ContentType,
		"etag":         info.ETag,
		"url":          info.URL,
		"actor":        ut.Actor,
	})
}
