package mcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/locks"
)

// Page-lock MCP tools. Both require an admin caller; non-admins get a
// structured error before the store is touched. The HTTP layer also
// gates /api/locks admin-only for defence in depth.

func (s *Server) toolLockPage(r *http.Request, args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Locks == nil {
		return nil, &RPCError{Code: -32000, Message: "locks store unavailable"}
	}
	if s.IsAdmin != nil && !s.IsAdmin(r) {
		return nil, &RPCError{Code: -32000, Message: "admin token required to lock pages"}
	}
	path := strings.TrimPrefix(strings.TrimSpace(getString(args, "path")), "/")
	if path == "" {
		return nil, &RPCError{Code: -32602, Message: "path required"}
	}
	if s.Pages == nil || s.Pages.GetPage(strings.TrimSuffix(path, ".md")) == nil {
		return nil, &RPCError{Code: -32000, Message: "page does not exist"}
	}
	actor := ""
	if s.ActorResolver != nil {
		actor = s.ActorResolver()
	}
	if actor == "" {
		actor = "mcp"
	}
	lock, err := s.Locks.Lock(locks.LockParams{
		Path:     path,
		LockedBy: actor,
		Reason:   getString(args, "reason"),
	})
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return lock, nil
}

func (s *Server) toolUnlockPage(r *http.Request, args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Locks == nil {
		return nil, &RPCError{Code: -32000, Message: "locks store unavailable"}
	}
	if s.IsAdmin != nil && !s.IsAdmin(r) {
		return nil, &RPCError{Code: -32000, Message: "admin token required to unlock pages"}
	}
	path := strings.TrimPrefix(strings.TrimSpace(getString(args, "path")), "/")
	if path == "" {
		return nil, &RPCError{Code: -32602, Message: "path required"}
	}
	if err := s.Locks.Unlock(path); err != nil {
		if errors.Is(err, locks.ErrNotFound) {
			return nil, &RPCError{Code: -32000, Message: "page is not locked"}
		}
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return map[string]any{"ok": true, "path": path}, nil
}
