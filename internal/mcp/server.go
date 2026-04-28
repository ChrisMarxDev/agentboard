package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/christophermarx/agentboard/internal/components"
	"github.com/christophermarx/agentboard/internal/data"
	interrors "github.com/christophermarx/agentboard/internal/errors"
	"github.com/christophermarx/agentboard/internal/files"
	"github.com/christophermarx/agentboard/internal/grab"
	"github.com/christophermarx/agentboard/internal/locks"
	"github.com/christophermarx/agentboard/internal/mdx"
	"github.com/christophermarx/agentboard/internal/store"
	"github.com/christophermarx/agentboard/internal/teams"
	"github.com/christophermarx/agentboard/internal/webhooks"
)

// Server implements the MCP Streamable HTTP transport.
type Server struct {
	Store                data.DataStore
	FileStore            *store.Store // files-first store; powers the agentboard_v2_* tools
	Pages                *mdx.PageManager
	Search               *mdx.SearchStore
	Components           *components.Manager
	Files                *files.Manager
	Errors               *interrors.Buffer
	Grab                 *grab.Materializer
	Webhooks             *webhooks.Store
	WebhookDispatcher    *webhooks.Dispatcher
	Teams                *teams.Store
	Locks                *locks.Store
	// IsAdmin reads the admin-ness of the current caller off an
	// HTTP request's context. Wired by the server that owns the auth
	// middleware. Nil → non-admin (defense in depth).
	IsAdmin              func(*http.Request) bool
	ActorResolver        func() string // returns current actor for attribution; optional
	AllowComponentUpload bool

	// MintUploadToken minds a one-shot presigned upload token for the
	// caller. Wired in by the HTTP server (mcp tests stub it). Returns
	// the public URL the agent should PUT bytes to and the expiry. Nil
	// means the upload feature isn't configured (returned to the agent
	// as a structured error).
	MintUploadToken func(name, actor string, sizeBytes int64) (uploadURL, expiresAt string, maxBytes int64, ok bool)
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
