package mcp

// MCP surface for the AgentBoard substrate. Four tools. The whole
// surface is git operations; no virtual types, no custom CRUD verbs,
// no event bus.
//
//   agentboard_workspaces            — list workspaces visible to this caller
//   agentboard_pull(ws, ref?)        — return the working tree as a bundle
//   agentboard_propose(ws, base?,
//                      branch?, files,
//                      message)      — server-side branch + commit + push
//   agentboard_resolve_conflict(
//       proposal, file, resolution)  — submit a resolved file body
//
// Agents that can shell out use `git` directly; those that can't
// (some sandboxed runtimes) use pull / propose / resolve_conflict.
// To react to peer pushes, agents re-pull on a cadence — the wiki
// audience doesn't need an event-bus protocol.

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *Server) toolDefinitions() []ToolDef {
	return []ToolDef{
		{
			Name:        "agentboard_workspaces",
			Description: "List the workspaces this caller can see. Each entry has {id, default_branch, policy, clone_url}. Call this first — `clone_url` is the value to pass to `git clone` (or to `agentboard_pull` for git-less runtimes). Spec §1.5 (the start-here flow) describes how the workspace teaches the rest.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "agentboard_pull",
			Description: "Return the working tree of `workspace` at `ref` (default: workspace's default branch) as a bundle: `{files: [{path, frontmatter?, body, sha}]}`. For agents whose runtime can't `git clone`. The bundle is read-only; to write back, use `agentboard_propose`. `README.md` at the root is always present and tells you how this workspace is organized — read it first.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workspace": map[string]string{"type": "string", "description": "Workspace id from agentboard_workspaces."},
					"ref":       map[string]string{"type": "string", "description": "Optional ref (branch, tag, sha). Defaults to the workspace's default branch."},
				},
				"required": []string{"workspace"},
			},
		},
		{
			Name:        "agentboard_propose",
			Description: "Server-side commit + push. Writes the given `files` onto a fresh branch (or directly onto the default branch in push-to-main mode), commits with `message`, pushes back. Returns `{success, branch, conflicts?}`. On conflict, the conflicting files are listed and you call `agentboard_resolve_conflict` for each.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workspace": map[string]string{"type": "string"},
					"base":      map[string]string{"type": "string", "description": "Optional base ref (defaults to workspace's default branch)."},
					"branch":    map[string]string{"type": "string", "description": "Optional explicit branch name. Auto-generated if absent."},
					"message":   map[string]string{"type": "string", "description": "Commit message."},
					"files": map[string]any{
						"type":        "array",
						"description": "List of {path, body} pairs. `body` is the full new file contents; deletion is `body: null`.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"path": map[string]string{"type": "string"},
								"body": map[string]string{"type": "string"},
							},
							"required": []string{"path"},
						},
						"minItems": 1,
					},
				},
				"required": []string{"workspace", "files", "message"},
			},
		},
		{
			Name:        "agentboard_resolve_conflict",
			Description: "Submit a resolved file body for a pending proposal that failed to merge. The server replays the merge with this file's resolution applied and retries the push. Repeat until no conflicts remain or the proposal is dropped.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"proposal":   map[string]string{"type": "string", "description": "Proposal id from a previous failed propose call."},
					"file":       map[string]string{"type": "string", "description": "Path that conflicted."},
					"resolution": map[string]string{"type": "string", "description": "The resolved file body (no <<<<<<<<< markers)."},
				},
				"required": []string{"proposal", "file", "resolution"},
			},
		},
	}
}

// handleToolCall dispatches a single tools/call invocation. Spec §6
// is the surface contract; this is the wiring.
func (s *Server) handleToolCall(r *http.Request, raw json.RawMessage) (any, *RPCError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: "bad params: " + err.Error()}
	}
	var args map[string]json.RawMessage
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return nil, &RPCError{Code: -32602, Message: "arguments must be a JSON object"}
		}
	}
	switch p.Name {
	case "agentboard_workspaces":
		return s.toolWorkspaces(r, args)
	case "agentboard_pull":
		return s.toolPull(r, args)
	case "agentboard_propose":
		return s.toolPropose(r, args)
	case "agentboard_resolve_conflict":
		return s.toolResolveConflict(r, args)
	}
	return nil, &RPCError{Code: -32601, Message: fmt.Sprintf("tool not found: %s", p.Name)}
}
