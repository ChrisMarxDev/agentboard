package mcp

// Cut 6: 10 tools cover the content tier. Always-plural batch shape;
// best-effort partial-success semantics; native JSON values; full
// envelope on read; non-blocking shape warnings on write. Spec §6.
//
// Read tier:
//   agentboard_read(paths)              — paths: [string]
//   agentboard_list(path)               — folder children + frontmatter snippets
//   agentboard_search(q, scope?)        — FTS + substring across the tree
//
// Write tier (always batch):
//   agentboard_write(items)             — items: [{path, frontmatter?, body?, version?}]
//   agentboard_patch(items)             — items: [{path, frontmatter_patch?, body?, version?}]
//   agentboard_append(path, items)      — items: [any]; one stream per call
//   agentboard_delete(items)            — items: [{path, version?}]
//
// Files:
//   agentboard_request_file_upload(items) — items: [{name, size_bytes}]
//
// Named extensions:
//   agentboard_grab(picks)              — cross-page materializer
//   agentboard_fire_event(event, body?) — emit on webhook bus

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *Server) toolDefinitions() []ToolDef {
	return []ToolDef{
		// ---------- Read tier ----------
		{
			Name:        "agentboard_read",
			Description: "Read one or more leaves by path. Always-plural batch — wrap a single read in a one-element array. Returns a `results` array; each entry has the full envelope (frontmatter + body + version) on success or a structured error. Paths resolve against the page tree first (e.g. `tasks/task-42`), then the data tier (e.g. `metrics/dau`). Body-only `read_page` is gone — every read returns the same shape.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"paths": map[string]any{
						"type":        "array",
						"description": "List of leaf paths to read. No `.md` suffix.",
						"items":       map[string]string{"type": "string"},
						"minItems":    1,
					},
				},
				"required": []string{"paths"},
			},
		},
		{
			Name:        "agentboard_list",
			Description: "List the children of a folder leaf. Pass `path: \"\"` to list the project root. Each child carries its frontmatter snippet so the caller can render a kanban / table without a follow-up read.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Folder path (no trailing slash). Pass empty string for the root.",
					},
				},
			},
		},
		{
			Name:        "agentboard_search",
			Description: "Full-text + substring search across the tree. Pass `scope: \"pages\"` or `\"data\"` to narrow; default is everything. Returns ranked hits with path, snippet, and score.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"q":     map[string]string{"type": "string", "description": "Whitespace-separated terms (implicit AND); quote for exact phrases."},
					"scope": map[string]string{"type": "string", "description": "pages | data | all (default)"},
					"limit": map[string]any{"type": "integer", "description": "Max hits (default 20, cap 100)"},
				},
				"required": []string{"q"},
			},
		},

		// ---------- Write tier (always batch) ----------
		{
			Name:        "agentboard_write",
			Description: "Create or replace one or more leaves. Always-plural batch; pass `items: [{path, frontmatter?, body?, version?}]`. Best-effort partial-success — inspect each result for success/error and retry the failures. `frontmatter` is native JSON (no double-stringify). When a path matches a suggested-shape glob (spec §8) and required fields are missing, the response includes a non-blocking `shape_hint` warning — the write still succeeds.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{
						"type":     "array",
						"minItems": 1,
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"path":        map[string]string{"type": "string"},
								"frontmatter": map[string]any{"type": "object", "description": "Native JSON object merged into the page's frontmatter (replacing any prior value)."},
								"body":        map[string]string{"type": "string", "description": "Optional MDX body. Omit for a frontmatter-only singleton."},
								"version":     map[string]string{"type": "string", "description": "Echo from a prior read (CAS) or '*' to force-overwrite. Omit on initial writes."},
							},
							"required": []string{"path"},
						},
					},
				},
				"required": []string{"items"},
			},
		},
		{
			Name:        "agentboard_patch",
			Description: "Merge frontmatter into one or more existing leaves and/or replace bodies. Always-plural batch; pass `items: [{path, frontmatter_patch?, body?, version?}]`. RFC-7396 shallow merge: `null` deletes a key, missing keys are preserved. `body` is optional — when set (even empty string) it replaces the body verbatim; when absent the body is preserved.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{
						"type":     "array",
						"minItems": 1,
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"path":              map[string]string{"type": "string"},
								"frontmatter_patch": map[string]any{"type": "object", "description": "Top-level keys merged into the existing frontmatter. null deletes a key."},
								"body":              map[string]string{"type": "string", "description": "Optional. Replaces the body when set."},
								"version":           map[string]string{"type": "string", "description": "Echo from a prior read (CAS) or '*' to force."},
							},
							"required": []string{"path"},
						},
					},
				},
				"required": []string{"items"},
			},
		},
		{
			Name:        "agentboard_append",
			Description: "Append items to a stream (`.ndjson`) leaf. One stream per call. Lock-free; never conflicts. Pass `items: [<any JSON value>]` — each becomes a timestamped line.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":  map[string]string{"type": "string", "description": "Stream path (no `.ndjson` suffix)."},
					"items": map[string]any{"type": "array", "minItems": 1, "description": "Values to append in order. Each is JSON-encoded as one stream line."},
				},
				"required": []string{"path", "items"},
			},
		},
		{
			Name:        "agentboard_delete",
			Description: "Delete one or more leaves. Always-plural batch; pass `items: [{path, version?}]`. Idempotent — already-gone paths return success. CAS via `version`.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{
						"type":     "array",
						"minItems": 1,
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"path":    map[string]string{"type": "string"},
								"version": map[string]string{"type": "string"},
							},
							"required": []string{"path"},
						},
					},
				},
				"required": []string{"items"},
			},
		},

		// ---------- Files ----------
		{
			Name:        "agentboard_request_file_upload",
			Description: "Mint one or more one-shot presigned upload URLs for binary files. Always-plural batch; pass `items: [{name, size_bytes?}]`. Each result carries `upload_url`, `expires_at`, and `max_size_bytes`. Agent shells out: `curl -X PUT --data-binary @<file> <upload_url>`. Keeps bytes off the JSON channel and out of the context window.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{
						"type":     "array",
						"minItems": 1,
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":       map[string]string{"type": "string", "description": "Filename, may contain nested path segments (exports/q1.csv)."},
								"size_bytes": map[string]any{"type": "integer", "description": "Expected size; rejected if it exceeds the per-project cap."},
							},
							"required": []string{"name"},
						},
					},
				},
				"required": []string{"items"},
			},
		},

		// ---------- Named extensions ----------
		{
			Name:        "agentboard_grab",
			Description: "Materialize a list of cross-page picks (Cards or headings) into a single agent-ready payload. Each pick is `{page, card_id?}` or `{page, heading_slug?, heading_level?}`. Returns `text` (markdown | xml | json) plus the resolved sections. Use to pull cross-page context when responding to the user.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"picks": map[string]any{
						"type":        "array",
						"description": "List of picks in render order.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"kind":          map[string]string{"type": "string", "description": "card | heading | page (default card)"},
								"page":          map[string]string{"type": "string", "description": "Page URL path, e.g. /features/auth"},
								"card_id":       map[string]string{"type": "string"},
								"heading_slug":  map[string]string{"type": "string"},
								"heading_level": map[string]any{"type": "integer"},
							},
							"required": []string{"page"},
						},
					},
					"format": map[string]string{"type": "string", "description": "markdown | xml | json (default markdown)"},
				},
				"required": []string{"picks"},
			},
		},
		{
			Name:        "agentboard_fire_event",
			Description: "Emit a user-triggered event onto the webhook bus. Every active subscription whose pattern matches `event` receives a signed POST. Useful for agent-triggered signals (\"deploy started\", \"runbook completed\") when data writes don't capture the intent. Subscription management lives on REST + CLI; this tool only fires events.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"event":   map[string]string{"type": "string", "description": "Event name, e.g. 'deploy.prod' or 'alert.pager'."},
					"payload": map[string]any{"type": "object", "description": "Structured payload carried as event.data."},
				},
				"required": []string{"event"},
			},
		},
	}
}

// handleToolCall dispatches one MCP tool call. The HTTP request is
// threaded through to every handler so they can resolve the bearer
// token to the calling user (Issue 7) for attribution.
func (s *Server) handleToolCall(r *http.Request, params json.RawMessage) (interface{}, *RPCError) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var args map[string]json.RawMessage
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
		}
	}
	if args == nil {
		args = map[string]json.RawMessage{}
	}

	switch call.Name {
	case "agentboard_read":
		return s.toolRead(r, args)
	case "agentboard_list":
		return s.toolList(r, args)
	case "agentboard_search":
		return s.toolSearch(r, args)
	case "agentboard_write":
		return s.toolWrite(r, args)
	case "agentboard_patch":
		return s.toolPatch(r, args)
	case "agentboard_append":
		return s.toolAppend(r, args)
	case "agentboard_delete":
		return s.toolDelete(r, args)
	case "agentboard_request_file_upload":
		return s.toolRequestFileUpload(r, args)
	case "agentboard_grab":
		return s.toolGrab(r, args)
	case "agentboard_fire_event":
		return s.toolFireEvent(r, args)
	}
	return nil, &RPCError{Code: -32601, Message: fmt.Sprintf("Unknown tool: %s", call.Name)}
}

// getString pulls a string arg from the marshalled-once map. Returns
// "" when the key is missing or doesn't decode as a string.
func getString(args map[string]json.RawMessage, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	var s string
	_ = json.Unmarshal(v, &s)
	return s
}

// mcpJSON wraps a structured value into MCP's text-content shape.
// MCP doesn't have a structured-data content type yet, so we stringify
// and let the agent re-parse — same pattern every other tool uses.
func mcpJSON(v any) any {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcpContent(fmt.Sprintf("error encoding result: %v", err))
	}
	return mcpContent(string(b))
}

// mcpContent wraps plain text into MCP's content-block shape.
func mcpContent(text string) interface{} {
	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}
}
