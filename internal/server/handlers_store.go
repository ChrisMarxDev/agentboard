package server

// Handlers for the files-first store described in spec-file-storage.md.
// Mounted at /api/v2 alongside the existing /api/data routes; once the
// frontend is migrated to consume the envelope shape (Phase 4) we
// retire the old routes (Phase 5).

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/store"
	"github.com/go-chi/chi/v5"
)

// actorFor extracts the username from the auth context. Anonymous
// callers should be blocked by middleware before they reach a v2
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
	Error           string          `json:"error"`            // snake_case code
	Message         string          `json:"message"`          // human-readable hint
	Current         *store.Envelope `json:"current,omitempty"` // for 412 / cas_mismatch
	YourVersion     string          `json:"your_version,omitempty"`
	CurrentVersion  string          `json:"current_version,omitempty"`
	ExpectedShape   string          `json:"expected_shape,omitempty"`
	ActualShape     string          `json:"actual_shape,omitempty"`
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
			Error: "wrong_shape",
			Message: "this key already has a different shape; pick the matching op or use a new key",
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

// ---------- Reads ----------

func (s *Server) handleStoreRead(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")

	cat, ok := s.FileStore.CatalogGet(key)
	if !ok {
		// Unknown key — try each shape and let the store report 404.
		env, err := s.FileStore.ReadSingleton(key)
		if err != nil {
			translateStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, env)
		return
	}

	switch cat.Shape {
	case store.ShapeSingleton:
		env, err := s.FileStore.ReadSingleton(key)
		if err != nil {
			translateStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, env)
	case store.ShapeCollection:
		items, err := s.FileStore.ListCollection(key)
		if err != nil {
			translateStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"_meta": map[string]any{
				"shape": store.ShapeCollection,
				"count": len(items),
				"key":   key,
			},
			"items": items,
		})
	case store.ShapeStream:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		opts := store.ReadStreamOpts{
			Limit: limit,
			Since: r.URL.Query().Get("since"),
			Until: r.URL.Query().Get("until"),
		}
		lines, err := s.FileStore.ReadStream(key, opts)
		if err != nil {
			translateStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"_meta": map[string]any{
				"shape":      store.ShapeStream,
				"line_count": len(lines),
				"key":        key,
			},
			"lines": lines,
		})
	default:
		writeError(w, http.StatusInternalServerError, "unknown_shape", "internal: catalog has no shape for "+key)
	}
}

func (s *Server) handleStoreReadItem(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	id := chi.URLParam(r, "id")
	env, err := s.FileStore.ReadItem(key, id)
	if err != nil {
		translateStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// ---------- Writes ----------

// writePayload is the body shape for SET / PUT writes:
// {"_meta": {"version": "..."}, "value": ...}. Either field may be
// omitted; missing _meta means "no version asserted".
type writePayload struct {
	Meta  *store.Meta     `json:"_meta,omitempty"`
	Value json.RawMessage `json:"value"`
}

func (p writePayload) version() string {
	if p.Meta == nil {
		return ""
	}
	return p.Meta.Version
}

func (s *Server) handleStoreSet(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	op := r.URL.Query().Get("op")
	if op != "" {
		s.handleStoreAction(w, r, key, "")
		return
	}

	var p writePayload
	if err := readJSONBody(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	if len(p.Value) == 0 {
		writeError(w, http.StatusBadRequest, "missing_value", `body must include {"value": ...}`)
		return
	}
	env, err := s.FileStore.Set(key, p.Value, headerOrBodyVersion(r, p.version()), actorFor(r))
	if err != nil {
		translateStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleStoreMerge(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	var p writePayload
	if err := readJSONBody(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	patch := p.Value
	if len(patch) == 0 {
		// Tolerate a bare-patch body as well — agents commonly omit the
		// envelope wrapper for merges and just send the patch directly.
		// Re-read; if body was top-level JSON, treat the whole body as
		// the patch.
		// Simpler: if we got a non-envelope JSON, fall through.
		writeError(w, http.StatusBadRequest, "missing_value", `body must be {"value": <patch>} (or top-level patch object)`)
		return
	}
	env, err := s.FileStore.Merge(key, patch, actorFor(r))
	if err != nil {
		translateStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleStoreDelete(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	cat, ok := s.FileStore.CatalogGet(key)
	if !ok {
		// Idempotent — already gone.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	version := r.URL.Query().Get("version")
	if version == "" {
		version = "*" // explicit force unless caller asks otherwise via query
	}
	actor := actorFor(r)
	var err error
	switch cat.Shape {
	case store.ShapeSingleton:
		err = s.FileStore.DeleteSingleton(key, version, actor)
	case store.ShapeCollection:
		// Wholesale collection delete requires confirm=true to avoid
		// accidental drops.
		if r.URL.Query().Get("confirm") != "true" {
			writeError(w, http.StatusBadRequest, "confirmation_required",
				"deleting a whole collection requires ?confirm=true")
			return
		}
		err = s.FileStore.DeleteCollection(key, actor)
	case store.ShapeStream:
		err = s.FileStore.DeleteStream(key, actor)
	}
	if err != nil {
		translateStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Per-item writes ----------

func (s *Server) handleStoreUpsert(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	id := chi.URLParam(r, "id")
	op := r.URL.Query().Get("op")
	if op != "" {
		s.handleStoreAction(w, r, key, id)
		return
	}

	var p writePayload
	if err := readJSONBody(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	if len(p.Value) == 0 {
		writeError(w, http.StatusBadRequest, "missing_value", `body must include {"value": ...}`)
		return
	}
	env, err := s.FileStore.UpsertItem(key, id, p.Value, headerOrBodyVersion(r, p.version()), actorFor(r))
	if err != nil {
		translateStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleStoreMergeItem(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	id := chi.URLParam(r, "id")
	var p writePayload
	if err := readJSONBody(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	if len(p.Value) == 0 {
		writeError(w, http.StatusBadRequest, "missing_value", `body must include {"value": <patch>}`)
		return
	}
	env, err := s.FileStore.MergeItem(key, id, p.Value, actorFor(r))
	if err != nil {
		translateStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleStoreDeleteItem(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	id := chi.URLParam(r, "id")
	version := r.URL.Query().Get("version")
	if version == "" {
		version = "*"
	}
	if err := s.FileStore.DeleteItem(key, id, version, actorFor(r)); err != nil {
		translateStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Action verbs (?op=...) ----------

// handleStoreAction handles the action-verb endpoints: append, increment,
// cas. Triggered when the corresponding write/upsert handler sees a
// non-empty ?op= query. Path: /api/data/{key}?op=... or
// /api/data/{key}/{id}?op=cas (item-level CAS).
func (s *Server) handleStoreAction(w http.ResponseWriter, r *http.Request, key, id string) {
	op := r.URL.Query().Get("op")
	actor := actorFor(r)

	switch strings.ToLower(op) {
	case "append":
		if id != "" {
			writeError(w, http.StatusBadRequest, "wrong_target", "append targets a stream key, not an item")
			return
		}
		var body struct {
			Value json.RawMessage   `json:"value"`
			Items []json.RawMessage `json:"items"`
		}
		if err := readJSONBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_body", err.Error())
			return
		}
		if body.Items != nil {
			lines, err := s.FileStore.AppendBatch(key, body.Items, actor)
			if err != nil {
				translateStoreError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"appended": len(lines), "lines": lines})
			return
		}
		if len(body.Value) == 0 {
			writeError(w, http.StatusBadRequest, "missing_value", `pass {"value": ...} or {"items": [...]}`)
			return
		}
		line, err := s.FileStore.Append(key, body.Value, actor)
		if err != nil {
			translateStoreError(w, err)
			return
		}
		// Inbox dispatch: @mentions inside the appended value land in the
		// mentioned user's inbox. Same path the singleton/collection
		// writes use, so a comment carrying "@dana please check" produces
		// an inbox row exactly like a page edit would.
		s.dispatchInboxForValueWrite(key, body.Value, actor, "")
		writeJSON(w, http.StatusOK, line)

	default:
		// `op=increment` and `op=cas` were removed in Cut 2 — agents
		// do read-modify-write against the file's _meta.version. The
		// only structured op now is `op=append` (streams).
		writeError(w, http.StatusBadRequest, "unknown_op",
			`?op must be one of: append (streams). Use PUT/PATCH for read-modify-write writes against the file-level CAS.`)
	}
}

// ---------- History + activity ----------

func (s *Server) handleStoreHistory(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := s.FileStore.ReadHistory(key, "", limit)
	if err != nil {
		translateStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key":     key,
		"entries": entries,
		"count":   len(entries),
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

// registerStoreRoutes mounts the store surface on the authenticated
// /api router. Skips registration when no FileStore is configured so
// the server still boots in legacy-only mode. Uses a Group instead of
// a Route("/", ...) — Group scopes middleware without nesting, which
// avoids chi's "trailing slash" conflict resolution that was eating
// /api/search registrations.
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

		r.Route("/data/{key}", func(r chi.Router) {
			r.Get("/", s.handleStoreRead)
			r.Put("/", s.handleStoreSet)
			r.Patch("/", s.handleStoreMerge)
			r.Post("/", s.handleStoreSet) // POST is the action-verb path (?op=...)
			r.Delete("/", s.handleStoreDelete)

			r.Get("/history", s.handleStoreHistory)

			r.Get("/{id}", s.handleStoreReadItem)
			r.Put("/{id}", s.handleStoreUpsert)
			r.Patch("/{id}", s.handleStoreMergeItem)
			r.Post("/{id}", s.handleStoreUpsert)
			r.Delete("/{id}", s.handleStoreDeleteItem)
		})
	})
}

// headerOrBodyVersion picks the version from If-Match (header) or the
// envelope's _meta.version (body). If both are present they must match;
// mismatch returns "" so the store rejects with a clear error.
func headerOrBodyVersion(r *http.Request, body string) string {
	hdr := strings.Trim(r.Header.Get("If-Match"), `"`)
	switch {
	case hdr == "" && body == "":
		return ""
	case hdr == "":
		return body
	case body == "":
		return hdr
	case hdr == body:
		return hdr
	default:
		return "" // contradiction — let store fail with VersionRequired
	}
}
