package mcp

// Cut 5 — MCP surface for the git substrate. Spec §6.
//
// The 10-tool batch-CRUD surface (read/list/search/write/patch/
// append/delete/request_file_upload) is retired. Agents who can shell
// out to `git` use it directly — clone, branch, commit, push. Agents
// whose runtime can't ship `git` use the workspaces/pull/propose/
// resolve_conflict triplet to do the same things server-side. Events
// flow through subscribe. Grab + fire_event carry over unchanged.
//
// Seven tools:
//
//   agentboard_workspaces            — list workspaces visible to this token
//   agentboard_pull(ws, ref?)        — return the working tree as a bundle
//   agentboard_propose(ws, base,
//                      branch, files,
//                      message)      — server-side branch + commit + push
//   agentboard_resolve_conflict(
//       proposal, file, resolution)  — submit a resolved file body
//   agentboard_subscribe(events)     — open an SSE-shaped event stream
//   agentboard_grab(picks)           — cross-page materializer (carryover)
//   agentboard_fire_event(event, …)  — webhook bus (carryover)
//
// Single-leaf reads / writes don't have dedicated tools — agents with
// `git` use `git show <ref>:<path>` / a branch + commit + push; agents
// without `git` either `pull` and pick a path out of the bundle, or
// `propose` a one-file change.

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
		{
			Name:        "agentboard_subscribe",
			Description: "Open a long-lived event stream of push / merge / conflict / mention events for workspaces this caller can read. The transport is MCP streaming. Useful for agents that want to react to pushes from peers in real time. (v1 returns a snapshot of recent activity; live streaming arrives in a later cut.)",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"events": map[string]any{
						"type":        "array",
						"description": "Event types to subscribe to. Empty = all.",
						"items":       map[string]string{"type": "string"},
					},
					"workspace": map[string]string{"type": "string", "description": "Optional filter to a single workspace."},
				},
			},
		},
		{
			Name:        "agentboard_grab",
			Description: "Cross-leaf materializer. Takes a list of `picks` (paths or globs) and returns one assembled text blob — frontmatter + body of each leaf concatenated with delimiters. The single canonical tool for 'gather the context I need to think about X.' Works against the workspace's working tree.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"workspace": map[string]string{"type": "string", "description": "Workspace id."},
					"picks": map[string]any{
						"type":     "array",
						"items":    map[string]string{"type": "string"},
						"minItems": 1,
					},
				},
				"required": []string{"picks"},
			},
		},
		{
			Name:        "agentboard_fire_event",
			Description: "Emit a user-defined event on the webhook bus. Any subscriber registered for this event name receives it. Useful for 'I finished step X; downstream agents, you can start now.' Management of subscribers (subscribe / list) lives on REST + CLI; this tool only dispatches.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"event": map[string]string{"type": "string"},
					"payload": map[string]any{
						"type":        "object",
						"description": "Arbitrary JSON delivered to subscribers.",
					},
				},
				"required": []string{"event"},
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
	case "agentboard_subscribe":
		return s.toolSubscribe(r, args)
	case "agentboard_grab":
		return s.toolGrab(r, args)
	case "agentboard_fire_event":
		return s.toolFireEvent(r, args)
	}
	return nil, &RPCError{Code: -32601, Message: fmt.Sprintf("tool not found: %s", p.Name)}
}
