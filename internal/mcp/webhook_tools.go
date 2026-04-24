package mcp

import (
	"encoding/json"
	"errors"

	"github.com/christophermarx/agentboard/internal/webhooks"
)

// toolListWebhooks → agentboard_list_webhooks
func (s *Server) toolListWebhooks() (interface{}, *RPCError) {
	if s.Webhooks == nil {
		return nil, &RPCError{Code: -32000, Message: "webhooks unavailable"}
	}
	list, err := s.Webhooks.ListActive()
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return map[string]interface{}{"subscriptions": list}, nil
}

// toolCreateWebhook → agentboard_create_webhook
func (s *Server) toolCreateWebhook(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Webhooks == nil {
		return nil, &RPCError{Code: -32000, Message: "webhooks unavailable"}
	}
	pattern := getString(args, "event_pattern")
	dest := getString(args, "destination_url")
	label := getString(args, "label")
	if pattern == "" || dest == "" {
		return nil, &RPCError{Code: -32602, Message: "event_pattern and destination_url required"}
	}
	actor := ""
	if s.ActorResolver != nil {
		actor = s.ActorResolver()
	}
	if actor == "" {
		actor = "mcp"
	}
	secret, sub, err := s.Webhooks.Create(webhooks.CreateParams{
		EventPattern:   pattern,
		DestinationURL: dest,
		Label:          label,
		CreatedBy:      actor,
	})
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return map[string]interface{}{
		"subscription": sub,
		"secret":       secret,
		"note":         "secret returned once — save it now; receivers verify via X-AgentBoard-Signature: sha256=<hmac>",
	}, nil
}

// toolRevokeWebhook → agentboard_revoke_webhook
func (s *Server) toolRevokeWebhook(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Webhooks == nil {
		return nil, &RPCError{Code: -32000, Message: "webhooks unavailable"}
	}
	id := getString(args, "id")
	if id == "" {
		return nil, &RPCError{Code: -32602, Message: "id required"}
	}
	if err := s.Webhooks.Revoke(id); err != nil {
		if errors.Is(err, webhooks.ErrNotFound) {
			return nil, &RPCError{Code: -32000, Message: "subscription not found"}
		}
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return map[string]interface{}{"ok": true, "id": id}, nil
}

// toolFireEvent → agentboard_fire_event
func (s *Server) toolFireEvent(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.WebhookDispatcher == nil {
		return nil, &RPCError{Code: -32000, Message: "webhook dispatcher unavailable"}
	}
	eventName := getString(args, "event")
	if eventName == "" {
		return nil, &RPCError{Code: -32602, Message: "event required"}
	}
	var payload map[string]any
	if raw, ok := args["payload"]; ok && len(raw) > 0 {
		_ = json.Unmarshal(raw, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if s.ActorResolver != nil {
		if actor := s.ActorResolver(); actor != "" {
			payload["fired_by"] = actor
		}
	}
	// Count subscribers that would match so the caller gets useful feedback.
	matched := 0
	if s.Webhooks != nil {
		if subs, err := s.Webhooks.ListActive(); err == nil {
			for _, sub := range subs {
				if webhooks.MatchEvent(sub.EventPattern, eventName) {
					matched++
				}
			}
		}
	}
	s.WebhookDispatcher.Emit(webhooks.Event{Name: eventName, Data: payload})
	return map[string]interface{}{
		"ok":          true,
		"event":       eventName,
		"subscribers": matched,
	}, nil
}
