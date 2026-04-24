package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/webhooks"
)

// handleCreateWebhook mints a new subscription. Body:
//
//	{"event_pattern":"data.*","destination_url":"...","label":"..."}
//
// Response includes the plaintext `secret` exactly once — subsequent
// GETs return only the hash.
func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Webhooks == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "webhooks unavailable")
		return
	}
	var body struct {
		EventPattern   string `json:"event_pattern"`
		DestinationURL string `json:"destination_url"`
		Label          string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body must be JSON {event_pattern, destination_url, label?}")
		return
	}
	actor := resolveActor(r)
	secret, sub, err := s.Webhooks.Create(webhooks.CreateParams{
		EventPattern:   body.EventPattern,
		DestinationURL: body.DestinationURL,
		Label:          body.Label,
		CreatedBy:      actor,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", err.Error())
		return
	}
	// Cache the plaintext so the dispatcher can sign future deliveries.
	s.webhookSecrets.Store(sub.ID, secret)
	respondJSON(w, http.StatusOK, map[string]any{
		"subscription": sub,
		"secret":       secret,
	})
}

// handleListWebhooks returns every active subscription. Admin sees
// everything; other callers see only their own.
func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if s.Webhooks == nil {
		respondJSON(w, http.StatusOK, []any{})
		return
	}
	list, err := s.Webhooks.ListActive()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	// Filter to caller unless admin.
	actor := resolveActor(r)
	isAdmin := false
	if u := auth.UserFromContext(r.Context()); u != nil && u.Kind == auth.KindAdmin {
		isAdmin = true
	}
	if !isAdmin {
		filtered := list[:0]
		for _, sub := range list {
			if sub.CreatedBy == actor {
				filtered = append(filtered, sub)
			}
		}
		list = filtered
	}
	respondJSON(w, http.StatusOK, list)
}

// handleGetWebhook reads one subscription. Authorised if the caller
// is admin or the creator.
func (s *Server) handleGetWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Webhooks == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "webhooks unavailable")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/webhooks/")
	id = strings.SplitN(id, "/", 2)[0]
	if id == "" {
		respondError(w, http.StatusBadRequest, "INVALID_ID", "id required")
		return
	}
	sub, err := s.Webhooks.Get(id)
	if err != nil {
		if errors.Is(err, webhooks.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "subscription not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if !canSeeWebhook(r, sub.CreatedBy) {
		respondError(w, http.StatusForbidden, "FORBIDDEN", "not the owner")
		return
	}
	respondJSON(w, http.StatusOK, sub)
}

// handleUpdateWebhook patches event_pattern, destination_url, or
// label. Secret is immutable — rotation means revoke + recreate.
func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Webhooks == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "webhooks unavailable")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/webhooks/")
	id = strings.SplitN(id, "/", 2)[0]
	sub, err := s.Webhooks.Get(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "subscription not found")
		return
	}
	if !canSeeWebhook(r, sub.CreatedBy) {
		respondError(w, http.StatusForbidden, "FORBIDDEN", "not the owner")
		return
	}
	var body struct {
		EventPattern   *string `json:"event_pattern,omitempty"`
		DestinationURL *string `json:"destination_url,omitempty"`
		Label          *string `json:"label,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body must be JSON")
		return
	}
	updated, err := s.Webhooks.Update(id, webhooks.UpdateParams{
		EventPattern:   body.EventPattern,
		DestinationURL: body.DestinationURL,
		Label:          body.Label,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// handleRevokeWebhook deletes (marks revoked) a subscription.
func (s *Server) handleRevokeWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Webhooks == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "webhooks unavailable")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/webhooks/")
	id = strings.SplitN(id, "/", 2)[0]
	sub, err := s.Webhooks.Get(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "subscription not found")
		return
	}
	if !canSeeWebhook(r, sub.CreatedBy) {
		respondError(w, http.StatusForbidden, "FORBIDDEN", "not the owner")
		return
	}
	if err := s.Webhooks.Revoke(id); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	// Drop the cached secret too — no point keeping it around.
	s.webhookSecrets.Delete(id)
	respondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleTestWebhook fires a single synchronous delivery against the
// subscription's destination URL with a synthetic event payload.
// Returns the HTTP status the destination answered with, so the
// operator gets immediate feedback.
//
//	POST /api/webhooks/{id}/test
func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Webhooks == nil || s.WebhookDispatcher == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "webhooks unavailable")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/webhooks/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[1] != "test" {
		respondError(w, http.StatusBadRequest, "INVALID_ID", "expected /api/webhooks/{id}/test")
		return
	}
	id := parts[0]
	sub, err := s.Webhooks.Get(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "subscription not found")
		return
	}
	if !canSeeWebhook(r, sub.CreatedBy) {
		respondError(w, http.StatusForbidden, "FORBIDDEN", "not the owner")
		return
	}
	secret, _ := s.webhookSecretFor(id)
	evt := webhooks.Event{
		Name: "test.ping",
		Data: map[string]any{
			"subscription_id": sub.ID,
			"note":            "This is a test delivery triggered from /api/webhooks/{id}/test.",
		},
	}
	code, postErr := s.WebhookDispatcher.DeliverOne(r.Context(), sub.ID, sub.DestinationURL, secret, evt)
	result := map[string]any{
		"status_code": code,
	}
	if postErr != nil {
		result["error"] = postErr.Error()
	}
	respondJSON(w, http.StatusOK, result)
}

// handleFireWebhook dispatches a user-triggered event through the
// webhook bus. Used by the <Button fires="..."> component but also
// directly callable by agents that want to produce ad-hoc events
// (e.g. "mark this deploy done", "signal that the runbook ran").
//
//	POST /api/webhooks/fire
//	body: {"event": "deploy.prod", "payload": {...}}
//
// The event name is the literal emitted to the dispatcher. Patterns
// on subscriptions decide who gets it. Response includes the count of
// subscribers the event reached so the UI can report "fired — N
// subscribers" without a follow-up query.
func (s *Server) handleFireWebhook(w http.ResponseWriter, r *http.Request) {
	if s.WebhookDispatcher == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "webhooks unavailable")
		return
	}
	var body struct {
		Event   string         `json:"event"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body must be JSON {event, payload?}")
		return
	}
	if strings.TrimSpace(body.Event) == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "event required")
		return
	}
	// Count matching active subs so the caller gets immediate feedback.
	matched := 0
	if s.Webhooks != nil {
		if subs, err := s.Webhooks.ListActive(); err == nil {
			for _, sub := range subs {
				if webhooks.MatchEvent(sub.EventPattern, body.Event) {
					matched++
				}
			}
		}
	}
	// Enrich the payload with who fired it so receivers can audit.
	payload := body.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payload["fired_by"] = resolveActor(r)
	s.WebhookDispatcher.Emit(webhooks.Event{
		Name: body.Event,
		Data: payload,
	})
	respondJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"event":       body.Event,
		"subscribers": matched,
	})
}

// handleAdminListWebhooks lists every subscription (revoked or not)
// instance-wide. Admin-only — scoped under /api/admin/*.
func (s *Server) handleAdminListWebhooks(w http.ResponseWriter, _ *http.Request) {
	if s.Webhooks == nil {
		respondJSON(w, http.StatusOK, []any{})
		return
	}
	list, err := s.Webhooks.ListAll()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, list)
}

// webhookSecretFor returns the cached plaintext secret for a
// subscription, if we still have it. Missing secrets happen after a
// restart — dispatcher then falls back to unsigned delivery, noted in
// the response. A production setup would fetch from a secret store.
func (s *Server) webhookSecretFor(id string) (string, bool) {
	v, ok := s.webhookSecrets.Load(id)
	if !ok {
		return "", false
	}
	str, _ := v.(string)
	return str, str != ""
}

// canSeeWebhook reports whether the caller may view/edit a
// subscription owned by `createdBy`. Admins can see any; others only
// their own.
func canSeeWebhook(r *http.Request, createdBy string) bool {
	actor := resolveActor(r)
	if u := auth.UserFromContext(r.Context()); u != nil && u.Kind == auth.KindAdmin {
		return true
	}
	return actor != "" && actor == createdBy
}
