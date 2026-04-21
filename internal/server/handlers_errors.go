package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/christophermarx/agentboard/internal/errors"
)

// maxErrorPayloadBytes caps the POST /api/errors body. Error strings are
// typically < 1 KB; 16 KB leaves room for stack traces while bounding abuse.
const maxErrorPayloadBytes = 16 * 1024

func (s *Server) handleListErrors(w http.ResponseWriter, r *http.Request) {
	if s.Errors == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "errors buffer not configured")
		return
	}
	respondJSON(w, http.StatusOK, s.Errors.List())
}

func (s *Server) handleRecordError(w http.ResponseWriter, r *http.Request) {
	if s.Errors == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "errors buffer not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxErrorPayloadBytes+1))
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}
	if int64(len(body)) > maxErrorPayloadBytes {
		respondError(w, http.StatusRequestEntityTooLarge, "VALUE_TOO_LARGE", "error payload too large")
		return
	}
	var in errors.Input
	if err := json.Unmarshal(body, &in); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body is not valid JSON")
		return
	}
	entry := s.Errors.Record(in)
	if entry == nil {
		// Empty Error field — no-op but still 200 so the client doesn't retry.
		respondJSON(w, http.StatusOK, map[string]bool{"ok": true, "recorded": false})
		return
	}

	// Broadcast so agents subscribed to SSE see errors immediately — matches the
	// live-dashboard semantic for data/pages/components/files.
	payload, _ := json.Marshal(entry)
	s.Broadcaster.Broadcast(SSEEvent{Type: "error-reported", Data: payload})

	respondJSON(w, http.StatusOK, entry)
}

func (s *Server) handleClearErrors(w http.ResponseWriter, r *http.Request) {
	if s.Errors == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "errors buffer not configured")
		return
	}

	// Support per-key deletion via ?key=XYZ — useful for "I fixed that one".
	if key := r.URL.Query().Get("key"); key != "" {
		removed := s.Errors.ClearByKey(key)
		if !removed {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "no error with that key")
			return
		}
		s.Broadcaster.Broadcast(SSEEvent{
			Type: "error-cleared",
			Data: []byte(`{"key":"` + key + `"}`),
		})
		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "removed": 1})
		return
	}

	n := s.Errors.Clear()
	s.Broadcaster.Broadcast(SSEEvent{
		Type: "error-cleared",
		Data: []byte(`{"all":true}`),
	})
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "removed": n})
}
