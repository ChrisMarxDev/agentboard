package mcp

// MCP tools for the files-first store (docs/archive/spec-file-storage.md §11). Ten
// tier-shaped tools replace the resource-CRUD surface for data access.
// Old `agentboard_set` etc. stay registered until Phase 5 retires
// them; agents picking up the new tools first get the cleaner shape.

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/christophermarx/agentboard/internal/store"
)

// storeToolDefs returns the new tier-shaped tool definitions. Caller folds
// these into toolDefinitions().
func (s *Server) storeToolDefs() []ToolDef {
	if s.FileStore == nil {
		return nil
	}
	return []ToolDef{
		{
			Name:        "agentboard_index",
			Description: "Tier 1 — return the catalog of every key in the project: shape, version, size. One call after wakeup to orient.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "agentboard_search",
			Description: "Tier 2 — substring search across all data values. Returns ranked snippets with paths.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"q":     map[string]string{"type": "string", "description": "Whitespace-separated query terms"},
					"limit": map[string]string{"type": "integer", "description": "Max results (default 20)"},
				},
				"required": []string{"q"},
			},
		},
		{
			Name:        "agentboard_read",
			Description: "Read a singleton, list a collection, or tail a stream. Pass `id` to read one collection item.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]string{"type": "string", "description": "Dotted key"},
					"id":  map[string]string{"type": "string", "description": "Item ID (collection only)"},
				},
				"required": []string{"key"},
			},
		},
		{
			Name:        "agentboard_write",
			Description: "Set a singleton or upsert a collection item. Pass `version` (from a prior read) for optimistic CAS, or '*' to force-overwrite.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":     map[string]string{"type": "string", "description": "Dotted key"},
					"id":      map[string]string{"type": "string", "description": "Item ID (omit for singleton)"},
					"value":   map[string]string{"description": "Any JSON value"},
					"version": map[string]string{"type": "string", "description": "Echo from prior read (CAS), or '*' to force"},
				},
				"required": []string{"key", "value"},
			},
		},
		{
			Name:        "agentboard_merge",
			Description: "Deep-merge an RFC 7396 patch into a singleton or collection item. Server-retried; never returns conflict.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":   map[string]string{"type": "string", "description": "Dotted key"},
					"id":    map[string]string{"type": "string", "description": "Item ID (omit for singleton)"},
					"patch": map[string]string{"description": "JSON object whose fields merge into the existing value; null deletes a field"},
				},
				"required": []string{"key", "patch"},
			},
		},
		{
			Name:        "agentboard_append",
			Description: "Append one (`value`) or many (`items`) lines to a stream. Each line is timestamped server-side. Lock-free; never conflicts.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":   map[string]string{"type": "string", "description": "Dotted key (must be a stream or new)"},
					"value": map[string]string{"description": "Single JSON value to append"},
					"items": map[string]any{"type": "array", "description": "Array of values to append in order"},
				},
				"required": []string{"key"},
			},
		},
		// agentboard_increment + agentboard_cas were removed in
		// Cut 2 — atomic field-level ops are out, agents read-modify-
		// write against the file's _meta.version. The file-level CAS
		// at the write op layer handles concurrent races.
		{
			Name:        "agentboard_delete",
			Description: "Delete a key (any shape) or one collection item. Idempotent. Pass `confirm: true` to delete a non-empty collection.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":     map[string]string{"type": "string", "description": "Dotted key"},
					"id":      map[string]string{"type": "string", "description": "Item ID (collection only)"},
					"confirm": map[string]string{"type": "boolean", "description": "Required to wholesale-delete a collection"},
				},
				"required": []string{"key"},
			},
		},
		{
			Name:        "agentboard_history",
			Description: "Return per-key (or per-item) write history. Up to 100 entries retained, oldest first.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":   map[string]string{"type": "string", "description": "Dotted key"},
					"id":    map[string]string{"type": "string", "description": "Item ID (collection only)"},
					"limit": map[string]string{"type": "integer", "description": "Cap on returned entries"},
				},
				"required": []string{"key"},
			},
		},
		{
			Name:        "agentboard_request_file_upload",
			Description: "Mint a one-shot presigned URL for a binary file upload. Agent shells out: `curl -X PUT --data-binary @file <upload_url>`. Use this instead of the legacy base64 path — keeps bytes off the MCP/JSON channel and out of the context window.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":         map[string]string{"type": "string", "description": "Filename. May contain nested path segments (e.g. exports/q1.csv)."},
					"size_bytes":   map[string]string{"type": "integer", "description": "Expected size; rejected if it would exceed the per-project cap."},
					"content_type": map[string]string{"type": "string", "description": "Advisory MIME (server still sniffs)."},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "agentboard_activity",
			Description: "Return the global write log filtered by actor / path_prefix / time range.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit":       map[string]string{"type": "integer"},
					"since":       map[string]string{"type": "string", "description": "RFC3339Nano lower bound (exclusive)"},
					"until":       map[string]string{"type": "string", "description": "RFC3339Nano upper bound (inclusive)"},
					"actor":       map[string]string{"type": "string", "description": "Exact match"},
					"path_prefix": map[string]string{"type": "string", "description": "Prefix match"},
				},
			},
		},
	}
}

// dispatchStore routes the agentboard_* tools. Returns (result, ok). ok
// is false when the tool name didn't match — caller falls through to
// the legacy switch.
func (s *Server) dispatchStore(name string, args map[string]json.RawMessage) (any, *RPCError, bool) {
	if s.FileStore == nil {
		return nil, nil, false
	}
	switch name {
	case "agentboard_index":
		return s.toolStoreIndex()
	case "agentboard_search":
		return s.toolStoreSearch(args)
	case "agentboard_read":
		return s.toolStoreRead(args)
	case "agentboard_write":
		return s.toolStoreWrite(args)
	case "agentboard_merge":
		return s.toolStoreMerge(args)
	case "agentboard_append":
		return s.toolStoreAppend(args)
	// agentboard_increment + _v2_cas dispatched here pre-Cut-2.
	case "agentboard_delete":
		return s.toolStoreDelete(args)
	case "agentboard_history":
		return s.toolStoreHistory(args)
	case "agentboard_activity":
		return s.toolStoreActivity(args)
	case "agentboard_request_file_upload":
		return s.toolStoreRequestFileUpload(args)
	}
	return nil, nil, false
}

// toolStoreRequestFileUpload returns a one-shot presigned URL the agent
// can PUT raw bytes to. The MintUploadToken closure is wired in by the
// HTTP server — tests can stub it. Returning the *URL* (not just a
// token) means the agent doesn't need to know the server's host.
func (s *Server) toolStoreRequestFileUpload(args map[string]json.RawMessage) (any, *RPCError, bool) {
	if s.MintUploadToken == nil {
		return nil, &RPCError{Code: -32000, Message: "upload subsystem not configured"}, true
	}
	name := getString(args, "name")
	if name == "" {
		return nil, &RPCError{Code: -32602, Message: "name required"}, true
	}
	var sizeBytes int64
	if raw, ok := args["size_bytes"]; ok && len(raw) > 0 {
		_ = json.Unmarshal(raw, &sizeBytes)
	}
	url, expiresAt, maxBytes, ok := s.MintUploadToken(name, s.actor(), sizeBytes)
	if !ok {
		return nil, &RPCError{Code: -32000, Message: "could not mint token (check size cap or filename rules)"}, true
	}
	return mcpJSON(map[string]any{
		"upload_url":     url,
		"expires_at":     expiresAt,
		"max_size_bytes": maxBytes,
		"hint":           "Shell out: curl -X PUT --data-binary @<path> <upload_url>",
	}), nil, true
}

// dispatchStore wrappers. Each tool returns the result body wrapped in
// MCP content; structured store errors translate to RPCError with
// embedded current state where applicable.

func (s *Server) toolStoreIndex() (any, *RPCError, bool) {
	cat := s.FileStore.Catalog()
	return mcpJSON(map[string]any{"data": cat, "count": len(cat)}), nil, true
}

func (s *Server) toolStoreSearch(args map[string]json.RawMessage) (any, *RPCError, bool) {
	q := getString(args, "q")
	if q == "" {
		return nil, &RPCError{Code: -32602, Message: "search needs a non-empty `q`"}, true
	}
	var limit int
	_ = json.Unmarshal(args["limit"], &limit)
	results, err := s.FileStore.Search(store.SearchOpts{Query: q, Limit: limit})
	if err != nil {
		return nil, storeRPCErr(err), true
	}
	return mcpJSON(map[string]any{"query": q, "results": results}), nil, true
}

func (s *Server) toolStoreRead(args map[string]json.RawMessage) (any, *RPCError, bool) {
	key := getString(args, "key")
	if key == "" {
		return nil, &RPCError{Code: -32602, Message: "key required"}, true
	}
	id := getString(args, "id")

	if id != "" {
		env, err := s.FileStore.ReadItem(key, id)
		if err != nil {
			return nil, storeRPCErr(err), true
		}
		return mcpJSON(env), nil, true
	}

	cat, ok := s.FileStore.CatalogGet(key)
	if !ok {
		return nil, storeRPCErr(store.ErrNotFound), true
	}
	switch cat.Shape {
	case store.ShapeSingleton:
		env, err := s.FileStore.ReadSingleton(key)
		if err != nil {
			return nil, storeRPCErr(err), true
		}
		return mcpJSON(env), nil, true
	case store.ShapeCollection:
		items, err := s.FileStore.ListCollection(key)
		if err != nil {
			return nil, storeRPCErr(err), true
		}
		return mcpJSON(map[string]any{"shape": "collection", "key": key, "items": items, "count": len(items)}), nil, true
	case store.ShapeStream:
		lines, err := s.FileStore.ReadStream(key, store.ReadStreamOpts{Limit: 100})
		if err != nil {
			return nil, storeRPCErr(err), true
		}
		return mcpJSON(map[string]any{"shape": "stream", "key": key, "lines": lines}), nil, true
	}
	return nil, &RPCError{Code: -32000, Message: "unknown shape"}, true
}

func (s *Server) toolStoreWrite(args map[string]json.RawMessage) (any, *RPCError, bool) {
	key := getString(args, "key")
	if key == "" {
		return nil, &RPCError{Code: -32602, Message: "key required"}, true
	}
	value := args["value"]
	if len(value) == 0 {
		return nil, &RPCError{Code: -32602, Message: "value required"}, true
	}
	version := getString(args, "version")
	id := getString(args, "id")
	actor := s.actor()

	var env *store.Envelope
	var err error
	if id != "" {
		env, err = s.FileStore.UpsertItem(key, id, value, version, actor)
	} else {
		env, err = s.FileStore.Set(key, value, version, actor)
	}
	if err != nil {
		return nil, storeRPCErr(err), true
	}
	return mcpJSON(env), nil, true
}

func (s *Server) toolStoreMerge(args map[string]json.RawMessage) (any, *RPCError, bool) {
	key := getString(args, "key")
	if key == "" {
		return nil, &RPCError{Code: -32602, Message: "key required"}, true
	}
	patch := args["patch"]
	if len(patch) == 0 {
		return nil, &RPCError{Code: -32602, Message: "patch required"}, true
	}
	id := getString(args, "id")
	actor := s.actor()

	var env *store.Envelope
	var err error
	if id != "" {
		env, err = s.FileStore.MergeItem(key, id, patch, actor)
	} else {
		env, err = s.FileStore.Merge(key, patch, actor)
	}
	if err != nil {
		return nil, storeRPCErr(err), true
	}
	return mcpJSON(env), nil, true
}

func (s *Server) toolStoreAppend(args map[string]json.RawMessage) (any, *RPCError, bool) {
	key := getString(args, "key")
	if key == "" {
		return nil, &RPCError{Code: -32602, Message: "key required"}, true
	}
	actor := s.actor()

	if items, ok := args["items"]; ok && len(items) > 0 {
		var values []json.RawMessage
		if err := json.Unmarshal(items, &values); err != nil {
			return nil, &RPCError{Code: -32602, Message: "items must be a JSON array"}, true
		}
		lines, err := s.FileStore.AppendBatch(key, values, actor)
		if err != nil {
			return nil, storeRPCErr(err), true
		}
		return mcpJSON(map[string]any{"appended": len(lines), "lines": lines}), nil, true
	}

	value := args["value"]
	if len(value) == 0 {
		return nil, &RPCError{Code: -32602, Message: "pass `value` (single) or `items` (batch)"}, true
	}
	line, err := s.FileStore.Append(key, value, actor)
	if err != nil {
		return nil, storeRPCErr(err), true
	}
	return mcpJSON(line), nil, true
}

// toolV2Increment + toolV2CAS removed in Cut 2 — agents read-modify-
// write against the file's _meta.version for atomicity, and the
// file-level CAS at the Set/UpsertItem layer covers concurrent races.

func (s *Server) toolStoreDelete(args map[string]json.RawMessage) (any, *RPCError, bool) {
	key := getString(args, "key")
	if key == "" {
		return nil, &RPCError{Code: -32602, Message: "key required"}, true
	}
	actor := s.actor()
	id := getString(args, "id")
	if id != "" {
		if err := s.FileStore.DeleteItem(key, id, "*", actor); err != nil {
			return nil, storeRPCErr(err), true
		}
		return mcpContent(fmt.Sprintf("Deleted %s/%s", key, id)), nil, true
	}

	cat, ok := s.FileStore.CatalogGet(key)
	if !ok {
		return mcpContent("Already gone"), nil, true
	}
	switch cat.Shape {
	case store.ShapeSingleton:
		if err := s.FileStore.DeleteSingleton(key, "*", actor); err != nil {
			return nil, storeRPCErr(err), true
		}
	case store.ShapeCollection:
		var confirm bool
		_ = json.Unmarshal(args["confirm"], &confirm)
		if !confirm {
			return nil, &RPCError{Code: -32602, Message: "deleting a non-empty collection requires confirm: true"}, true
		}
		if err := s.FileStore.DeleteCollection(key, actor); err != nil {
			return nil, storeRPCErr(err), true
		}
	case store.ShapeStream:
		if err := s.FileStore.DeleteStream(key, actor); err != nil {
			return nil, storeRPCErr(err), true
		}
	}
	return mcpContent(fmt.Sprintf("Deleted %s", key)), nil, true
}

func (s *Server) toolStoreHistory(args map[string]json.RawMessage) (any, *RPCError, bool) {
	key := getString(args, "key")
	if key == "" {
		return nil, &RPCError{Code: -32602, Message: "key required"}, true
	}
	id := getString(args, "id")
	var limit int
	_ = json.Unmarshal(args["limit"], &limit)
	entries, err := s.FileStore.ReadHistory(key, id, limit)
	if err != nil {
		return nil, storeRPCErr(err), true
	}
	return mcpJSON(map[string]any{"key": key, "id": id, "entries": entries, "count": len(entries)}), nil, true
}

func (s *Server) toolStoreActivity(args map[string]json.RawMessage) (any, *RPCError, bool) {
	var limit int
	_ = json.Unmarshal(args["limit"], &limit)
	entries, err := s.FileStore.ReadActivity(store.ReadActivityOpts{
		Limit:      limit,
		Since:      getString(args, "since"),
		Until:      getString(args, "until"),
		Actor:      getString(args, "actor"),
		PathPrefix: getString(args, "path_prefix"),
	})
	if err != nil {
		return nil, storeRPCErr(err), true
	}
	return mcpJSON(map[string]any{"entries": entries, "count": len(entries)}), nil, true
}

// actor returns the resolved caller name for attribution. Falls back to
// "agent" when no resolver is configured (matches the v2 REST default).
func (s *Server) actor() string {
	if s.ActorResolver != nil {
		if a := s.ActorResolver(); a != "" {
			return a
		}
	}
	return "agent"
}

// storeRPCErr translates store sentinel errors into structured MCP
// errors. Conflict + CAS responses include the current envelope inline
// in the message (JSON-encoded) so the agent reasoning over the error
// can reconcile in one round-trip — same poka-yoke as the REST 412.
func storeRPCErr(err error) *RPCError {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return &RPCError{Code: -32001, Message: "not_found: no value at this key"}
	case errors.Is(err, store.ErrVersionRequired):
		return &RPCError{Code: -32002, Message: `version_required: include "version" arg from a prior read, or pass "*" to force-overwrite`}
	case errors.Is(err, store.ErrLineTooLong):
		return &RPCError{Code: -32003, Message: "line_too_long: stream lines must be <= 4096 bytes; split or store as singleton"}
	case errors.Is(err, store.ErrInvalidValue):
		return &RPCError{Code: -32004, Message: "invalid_value: " + err.Error()}
	}
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		blob, _ := json.Marshal(map[string]any{
			"current":         conflict.Current,
			"your_version":    conflict.YourVersion,
			"current_version": currentVersion(conflict.Current),
		})
		return &RPCError{Code: -32005, Message: "version_mismatch: " + string(blob)}
	}
	var cas *store.CASError
	if errors.As(err, &cas) {
		blob, _ := json.Marshal(map[string]any{
			"current":         cas.Current,
			"current_version": currentVersion(cas.Current),
		})
		return &RPCError{Code: -32006, Message: "cas_mismatch: " + string(blob)}
	}
	var ws *store.WrongShapeError
	if errors.As(err, &ws) {
		return &RPCError{Code: -32007, Message: fmt.Sprintf(
			"wrong_shape: this key is %s; you tried %s. Use the matching op or pick a new key.",
			ws.Actual, ws.Attempt,
		)}
	}
	return &RPCError{Code: -32000, Message: err.Error()}
}

func currentVersion(env *store.Envelope) string {
	if env == nil {
		return ""
	}
	return env.Meta.Version
}

// mcpJSON returns an MCP content block with `data` JSON-encoded as
// text. MCP doesn't have a structured-data content type yet, so we
// stringify and let the agent re-parse — same pattern as every other
// tool in this server.
func mcpJSON(v any) any {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcpContent(fmt.Sprintf("error encoding result: %v", err))
	}
	return mcpContent(string(b))
}
