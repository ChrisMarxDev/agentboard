package server

import (
	"errors"
	"io"
	"net/http"

	"github.com/christophermarx/agentboard/internal/components"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleListComponents(w http.ResponseWriter, r *http.Request) {
	catalog := s.Components.ListComponents()
	respondJSON(w, http.StatusOK, catalog)
}

func (s *Server) handleComponentsBundle(w http.ResponseWriter, r *http.Request) {
	bundle := s.Components.GetBundle()
	w.Header().Set("Content-Type", "application/javascript")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(bundle))
}

func (s *Server) handleGetComponent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	source := s.Components.GetComponentSource(name)
	if source == "" {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "component not found: "+name)
		return
	}

	w.Header().Set("Content-Type", "application/javascript")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(source))
}

func (s *Server) handleWriteComponent(w http.ResponseWriter, r *http.Request) {
	if !s.AllowComponentUpload {
		respondError(w, http.StatusForbidden, "FORBIDDEN",
			"component upload is disabled. Start the server with --allow-component-upload or set allow_component_upload: true in agentboard.yaml.")
		return
	}

	name := chi.URLParam(r, "name")
	body, err := io.ReadAll(io.LimitReader(r.Body, components.MaxComponentSourceBytes+1))
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}

	if err := s.Components.WriteComponent(name, string(body)); err != nil {
		switch {
		case errors.Is(err, components.ErrInvalidComponentName):
			respondError(w, http.StatusBadRequest, "INVALID_KEY", err.Error())
		case errors.Is(err, components.ErrComponentTooLarge):
			respondError(w, http.StatusRequestEntityTooLarge, "VALUE_TOO_LARGE", err.Error())
		case errors.Is(err, components.ErrBuiltinComponent):
			respondError(w, http.StatusConflict, "BUILTIN_COMPONENT", err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
		return
	}

	s.Broadcaster.Broadcast(SSEEvent{
		Type: "components-updated",
		Data: []byte(`{"names":["` + name + `"]}`),
	})

	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "name": name})
}

func (s *Server) handleDeleteComponent(w http.ResponseWriter, r *http.Request) {
	if !s.AllowComponentUpload {
		respondError(w, http.StatusForbidden, "FORBIDDEN",
			"component upload is disabled. Start the server with --allow-component-upload or set allow_component_upload: true in agentboard.yaml.")
		return
	}

	name := chi.URLParam(r, "name")
	if err := s.Components.DeleteComponent(name); err != nil {
		switch {
		case errors.Is(err, components.ErrInvalidComponentName):
			respondError(w, http.StatusBadRequest, "INVALID_KEY", err.Error())
		case errors.Is(err, components.ErrBuiltinComponent):
			respondError(w, http.StatusConflict, "BUILTIN_COMPONENT", err.Error())
		case errors.Is(err, components.ErrComponentNotFound):
			respondError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
		return
	}

	s.Broadcaster.Broadcast(SSEEvent{
		Type: "components-updated",
		Data: []byte(`{"names":[]}`),
	})

	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
