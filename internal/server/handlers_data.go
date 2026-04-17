package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func getSource(r *http.Request) string {
	source := r.Header.Get("X-Agent-Source")
	if source == "" {
		source = "anonymous"
	}
	return source
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

	if err := s.Store.Set(key, body, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
	})
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

	if err := s.Store.Merge(key, body, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
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

	if err := s.Store.Append(key, body, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (s *Server) handleDeleteData(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	source := getSource(r)

	if err := s.Store.Delete(key, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
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

	if err := s.Store.UpsertById(key, id, body, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
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

	if err := s.Store.MergeById(key, id, body, source); err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (s *Server) handleDeleteById(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	id := chi.URLParam(r, "id")
	source := getSource(r)

	if err := s.Store.DeleteById(key, id, source); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (s *Server) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	schema, err := s.Store.InferSchema()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, schema)
}
