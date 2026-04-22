package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/data"
	"github.com/go-chi/chi/v5"
)

func getSource(r *http.Request) string {
	source := r.Header.Get("X-Agent-Source")
	if source == "" {
		source = "anonymous"
	}
	return source
}

// ifMatch extracts the optimistic-concurrency expected value. Strips weak-
// etag prefix + surrounding quotes per HTTP spec so `If-Match: "abc"` and
// `If-Match: abc` both work.
func ifMatch(r *http.Request) string {
	v := strings.TrimSpace(r.Header.Get("If-Match"))
	v = strings.TrimPrefix(v, "W/")
	v = strings.Trim(v, `"`)
	return v
}

// respondStale emits a 412 with the current meta so the caller can re-base
// their write and retry. meta may be nil when the key doesn't exist.
func respondStale(w http.ResponseWriter, s *Server, key string) {
	meta, _ := s.Store.GetMeta(key)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPreconditionFailed)
	payload := map[string]any{
		"code":  "STALE_WRITE",
		"error": "If-Match did not match current version",
		"key":   key,
	}
	if meta != nil {
		payload["current"] = meta
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleGetAllData(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	keysParam := r.URL.Query().Get("keys")

	var keys []string
	if keysParam != "" {
		keys = strings.Split(keysParam, ",")
	}

	result, err := s.Store.GetAll(prefix, keys)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetData(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")

	meta, err := s.Store.GetMeta(key)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if meta == nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "key not found: "+key)
		return
	}

	// Echo updated_at as the ETag so clients can round-trip it as If-Match on
	// their next write without a separate lookup.
	w.Header().Set("ETag", `"`+meta.UpdatedAt+`"`)
	respondJSON(w, http.StatusOK, meta)
}

func (s *Server) handleSetData(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	source := getSource(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}

	if !json.Valid(body) {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body is not valid JSON")
		return
	}

	if len(body) > 1*1024*1024 {
		respondError(w, http.StatusRequestEntityTooLarge, "VALUE_TOO_LARGE", "value exceeds 1 MB limit")
		return
	}

	if expected := ifMatch(r); expected != "" {
		if err := s.Store.SetIfMatch(key, body, source, expected); err != nil {
			if errors.Is(err, data.ErrStale) || errors.Is(err, data.ErrNotFoundForMatch) {
				respondStale(w, s, key)
				return
			}
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	} else if err := s.Store.Set(key, body, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	meta, _ := s.Store.GetMeta(key)
	resp := map[string]any{"ok": true}
	if meta != nil {
		resp["updated_at"] = meta.UpdatedAt
		w.Header().Set("ETag", `"`+meta.UpdatedAt+`"`)
	}
	respondJSON(w, http.StatusOK, resp)
}

func (s *Server) handleMergeData(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	source := getSource(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}

	if !json.Valid(body) {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body is not valid JSON")
		return
	}

	if expected := ifMatch(r); expected != "" {
		if err := s.Store.MergeIfMatch(key, body, source, expected); err != nil {
			if errors.Is(err, data.ErrStale) || errors.Is(err, data.ErrNotFoundForMatch) {
				respondStale(w, s, key)
				return
			}
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	} else if err := s.Store.Merge(key, body, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	meta, _ := s.Store.GetMeta(key)
	resp := map[string]any{"ok": true}
	if meta != nil {
		resp["updated_at"] = meta.UpdatedAt
		w.Header().Set("ETag", `"`+meta.UpdatedAt+`"`)
	}
	respondJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAppendData(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	source := getSource(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}

	if !json.Valid(body) {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body is not valid JSON")
		return
	}

	if expected := ifMatch(r); expected != "" {
		if err := s.Store.AppendIfMatch(key, body, source, expected); err != nil {
			if errors.Is(err, data.ErrStale) || errors.Is(err, data.ErrNotFoundForMatch) {
				respondStale(w, s, key)
				return
			}
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	} else if err := s.Store.Append(key, body, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	meta, _ := s.Store.GetMeta(key)
	resp := map[string]any{"ok": true}
	if meta != nil {
		resp["updated_at"] = meta.UpdatedAt
		w.Header().Set("ETag", `"`+meta.UpdatedAt+`"`)
	}
	respondJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeleteData(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	source := getSource(r)

	if expected := ifMatch(r); expected != "" {
		if err := s.Store.DeleteIfMatch(key, source, expected); err != nil {
			if errors.Is(err, data.ErrStale) || errors.Is(err, data.ErrNotFoundForMatch) {
				respondStale(w, s, key)
				return
			}
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	} else if err := s.Store.Delete(key, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleGetDataById(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	id := chi.URLParam(r, "id")

	item, err := s.Store.GetById(key, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if item == nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "item not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(item)
}

func (s *Server) handleUpsertById(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	id := chi.URLParam(r, "id")
	source := getSource(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}

	if !json.Valid(body) {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body is not valid JSON")
		return
	}

	if expected := ifMatch(r); expected != "" {
		if err := s.Store.UpsertByIdIfMatch(key, id, body, source, expected); err != nil {
			if errors.Is(err, data.ErrStale) || errors.Is(err, data.ErrNotFoundForMatch) {
				respondStale(w, s, key)
				return
			}
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	} else if err := s.Store.UpsertById(key, id, body, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	meta, _ := s.Store.GetMeta(key)
	resp := map[string]any{"ok": true}
	if meta != nil {
		resp["updated_at"] = meta.UpdatedAt
		w.Header().Set("ETag", `"`+meta.UpdatedAt+`"`)
	}
	respondJSON(w, http.StatusOK, resp)
}

func (s *Server) handleMergeById(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	id := chi.URLParam(r, "id")
	source := getSource(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}

	if !json.Valid(body) {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body is not valid JSON")
		return
	}

	if expected := ifMatch(r); expected != "" {
		if err := s.Store.MergeByIdIfMatch(key, id, body, source, expected); err != nil {
			if errors.Is(err, data.ErrStale) || errors.Is(err, data.ErrNotFoundForMatch) {
				respondStale(w, s, key)
				return
			}
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	} else if err := s.Store.MergeById(key, id, body, source); err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	meta, _ := s.Store.GetMeta(key)
	resp := map[string]any{"ok": true}
	if meta != nil {
		resp["updated_at"] = meta.UpdatedAt
		w.Header().Set("ETag", `"`+meta.UpdatedAt+`"`)
	}
	respondJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeleteById(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	id := chi.URLParam(r, "id")
	source := getSource(r)

	if expected := ifMatch(r); expected != "" {
		if err := s.Store.DeleteByIdIfMatch(key, id, source, expected); err != nil {
			if errors.Is(err, data.ErrStale) || errors.Is(err, data.ErrNotFoundForMatch) {
				respondStale(w, s, key)
				return
			}
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	} else if err := s.Store.DeleteById(key, id, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	schema, err := s.Store.InferSchema()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, schema)
}
