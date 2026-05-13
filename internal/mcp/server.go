package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/files"
	"github.com/christophermarx/agentboard/internal/gitserver"
	"github.com/christophermarx/agentboard/internal/grab"
	"github.com/christophermarx/agentboard/internal/webhooks"
)

// Server implements the MCP Streamable HTTP transport.
//
// Cut 6 collapsed the surface from 38 tools across 10 domains down
// to the spec §6 ten: 8 generic batch CRUD + agentboard_grab +
// agentboard_fire_event. Admin domains (teams, locks, webhook
// subscriptions) moved to REST + CLI per the AUTH.md MCP invariant
// (admin operations never expose through MCP).
type Server struct {
	// GitStore is the workspace registry — agentboard_workspaces and
	// agentboard_pull read from it directly. Required for those tools
	// to function; absent during early-pivot bring-up.
	GitStore *gitserver.Store

	// ProposeFn is the server-side implementation of
	// agentboard_propose. Wired by cli/serve.go. Nil → tool returns a
	// "not configured" error.
	ProposeFn ProposeFunc

	// ResolveConflictFn — server-side impl of agentboard_resolve_conflict.
	// Looks up the pending proposal, applies the file resolution,
	// retries the push when the conflict set empties.
	ResolveConflictFn ResolveConflictFunc

	// SubscribeFn — server-side impl of agentboard_subscribe. Polling
	// transport: returns events newer than `since` from the gitserver
	// events table.
	SubscribeFn SubscribeFunc

	// PublicBaseURL is what agentboard_workspaces uses to build
	// `clone_url`. Empty falls back to the inbound request's scheme +
	// host.
	PublicBaseURL string

	// Files backs agentboard_request_file_upload (deferred — file
	// uploads happen via git LFS or just as ordinary blobs now).
	// Kept on the struct for compatibility with serve.go wiring; not
	// dispatched in the Cut-5 surface.
	Files *files.Manager

	// Grab is the cross-page materializer behind agentboard_grab.
	Grab *grab.Materializer

	// WebhookDispatcher backs agentboard_fire_event.
	WebhookDispatcher *webhooks.Dispatcher

	// Auth: tool-side bearer-to-user resolution for commit attribution.
	Auth *auth.Store

	// MintUploadToken — kept on the struct for serve.go wiring; not
	// dispatched in the Cut-5 surface.
	MintUploadToken func(name, actor string, sizeBytes int64) (uploadURL, expiresAt string, maxBytes int64, ok bool)

	// AfterPageWrite — kept on the struct for serve.go wiring; the new
	// surface routes writes through ProposeFn instead.
	AfterPageWrite func(path, source, actor string)
}

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolDef defines an MCP tool.
type ToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// ServeHTTP handles MCP requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// SSE endpoint for MCP — return method not allowed for now
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "mcp_ready"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, -32700, "Parse error")
		return
	}

	var resp JSONRPCResponse
	resp.JSONRPC = "2.0"
	resp.ID = req.ID

	switch req.Method {
	case "initialize":
		resp.Result = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{
				"name":    "agentboard",
				"version": "0.1.0",
			},
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
		}

	case "notifications/initialized":
		// No response needed for notifications
		w.WriteHeader(http.StatusOK)
		return

	case "tools/list":
		resp.Result = map[string]interface{}{
			"tools": s.toolDefinitions(),
		}

	case "tools/call":
		result, rpcErr := s.handleToolCall(r, req.Params)
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}

	default:
		resp.Error = &RPCError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func writeRPCError(w http.ResponseWriter, id interface{}, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// resolveActor pulls the authenticated user off the request context
// (set by auth.TokenMiddleware higher up the chain). Returns "agent"
// when no user is attached — matches the previous default while
// closing Issue 7 (MCP writes attribute to the actual user, not the
// generic "agent" string).
func (s *Server) resolveActor(r *http.Request) string {
	if r != nil {
		if u := auth.UserFromContext(r.Context()); u != nil && u.Username != "" {
			return u.Username
		}
	}
	return "agent"
}
