package server

// Cross-cutting store endpoints: catalog, search, activity, mint
// presigned upload tokens. Per-leaf CRUD moved to the unified
// `/api/<path>` namespace in Cut 8 (handlers_unified.go) so this
// file no longer mounts `/api/data/<key>` routes — only the
// side-channel verbs that aren't tied to a single leaf. The shared
// helpers (translateStoreError, writeJSON, writeError, actorFor,
// errorBody, readJSONBody) are still exported for the unified
// handler.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/store"
	"github.com/go-chi/chi/v5"
)

// actorFor extracts the username from the auth context. Anonymous
// callers should be blocked by middleware before they reach a store
// handler — defensively we fall back to "anonymous" so the activity
// log always has *something* recognizable.
func actorFor(r *http.Request) string {
	if u := auth.UserFromContext(r.Context()); u != nil {
		return u.Username
	}
	return "anonymous"
}

// readJSONBody decodes a request body into dst, capping at 1 MiB to
// match the singleton write spec (binary content goes through the
// presigned-URL flow, not /api/data).
func readJSONBody(r *http.Request, dst any) error {
	defer r.Body.Close()
	const maxBody = 1 << 20
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return err
	}
	if len(body) > maxBody {
		return errors.New("body too large (max 1 MiB)")
	}
	if len(body) == 0 {
		return errors.New("empty body")
	}
	return json.Unmarshal(body, dst)
}

// writeJSON writes a JSON response with the given status. If the body
// is *Envelope, the version is also surfaced as an ETag header so
// HTTP-aware clients (browsers, fetch) can do conditional requests.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	if env, ok := body.(*store.Envelope); ok && env != nil {
		w.Header().Set("ETag", `"`+env.Meta.Version+`"`)
	}
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(body)
}

// errorBody is the shape of every 4xx/5xx response. Honors
// CORE_GUIDELINES §12: every error explains what went wrong, what
// was expected, and how to retry.
type errorBody struct {
	Error          string          `json:"error"`             // snake_case code
	Message        string          `json:"message"`           // human-readable hint
	Current        *store.Envelope `json:"current,omitempty"` // for 412 / cas_mismatch
	YourVersion    string          `json:"your_version,omitempty"`
	CurrentVersion string          `json:"current_version,omitempty"`
	ExpectedShape  string          `json:"expected_shape,omitempty"`
	ActualShape    string          `json:"actual_shape,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Error: code, Message: msg})
}

// translateStoreError maps store sentinel errors to (status, body).
// Centralizes the spec-mandated response shapes so each handler is a
// thin wrapper around a store call.
func translateStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no value at this key")
		return
	case errors.Is(err, store.ErrVersionRequired):
		writeError(w, http.StatusConflict, "version_required",
			`include "_meta": {"version": "<from prior read>"} in the body to write to an existing key, or set "_meta": {"version": "*"} to force-overwrite`)
		return
	case errors.Is(err, store.ErrLineTooLong):
		writeError(w, http.StatusRequestEntityTooLarge, "line_too_long",
			"stream lines must be ≤ 4096 bytes; split the value or store it as a singleton")
		return
	case errors.Is(err, store.ErrInvalidValue):
		writeError(w, http.StatusBadRequest, "invalid_value", err.Error())
		return
	}
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		body := errorBody{
			Error:       "version_mismatch",
			Message:     "this key was modified since your read; reconcile with current and retry with the current version",
			Current:     conflict.Current,
			YourVersion: conflict.YourVersion,
		}
		if conflict.Current != nil {
			body.CurrentVersion = conflict.Current.Meta.Version
			w.Header().Set("ETag", `"`+conflict.Current.Meta.Version+`"`)
		}
		writeJSON(w, http.StatusPreconditionFailed, body)
		return
	}
	var cas *store.CASError
	if errors.As(err, &cas) {
		body := errorBody{
			Error:   "cas_mismatch",
			Message: "the expected value did not equal the current value; check current and retry",
			Current: cas.Current,
		}
		if cas.Current != nil {
			body.CurrentVersion = cas.Current.Meta.Version
		}
		writeJSON(w, http.StatusConflict, body)
		return
	}
	var ws *store.WrongShapeError
	if errors.As(err, &ws) {
		writeJSON(w, http.StatusConflict, errorBody{
			Error:         "wrong_shape",
			Message:       "this key already has a different shape; pick the matching op or use a new key",
			ExpectedShape: ws.Attempt,
			ActualShape:   ws.Actual,
		})
		return
	}
	// Last resort.
	writeError(w, http.StatusInternalServerError, "store_error", err.Error())
}

// ---------- Tier 1: index ----------

func (s *Server) handleStoreIndex(w http.ResponseWriter, r *http.Request) {
	cat := s.FileStore.Catalog()
	writeJSON(w, http.StatusOK, map[string]any{
		"data":  cat,
		"count": len(cat),
	})
}

// ---------- Tier 2: search ----------

func (s *Server) handleStoreSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "missing_query", `pass ?q=<search terms>`)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	results, err := s.FileStore.Search(store.SearchOpts{
		Query: q,
		Scope: r.URL.Query().Get("scope"),
		Limit: limit,
	})
	if err != nil {
		translateStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query":   q,
		"results": results,
	})
}

func (s *Server) handleStoreActivity(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := s.FileStore.ReadActivity(store.ReadActivityOpts{
		Limit:      limit,
		Since:      r.URL.Query().Get("since"),
		Until:      r.URL.Query().Get("until"),
		Actor:      r.URL.Query().Get("actor"),
		PathPrefix: r.URL.Query().Get("path_prefix"),
	})
	if err != nil {
		translateStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"count":   len(entries),
	})
}

// registerStoreRoutes mounts the cross-cutting store endpoints
// (catalog, search, activity, presigned-upload mint). The per-leaf
// CRUD routes that used to live under `/data/{key}` got retired in
// Cut 8 — leaves now live at `/api/<path>` through
// handlers_unified.go. The four side-channel verbs below stay
// because they aren't tied to a single leaf:
//
//	GET  /api/index               — flat catalog of every leaf
//	GET  /api/search?q=...        — substring search across values
//	GET  /api/activity            — global write log
//	POST /api/files/request-upload — mint a presigned upload URL
//
// Skips registration when no FileStore is configured.
func (s *Server) registerStoreRoutes(r chi.Router) {
	if s.FileStore == nil {
		return
	}
	r.Group(func(r chi.Router) {
		r.Use(s.storeRateLimit)

		r.Get("/index", s.handleStoreIndex)
		r.Get("/search", s.handleStoreSearch)
		r.Get("/activity", s.handleStoreActivity)
		r.Post("/files/request-upload", s.handleRequestFileUpload)
	})
}
