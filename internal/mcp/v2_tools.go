package mcp

// MCP tools for the files-first store (spec-file-storage.md §11). Ten
// tier-shaped tools replace the resource-CRUD surface for data access.
// Old `agentboard_set` etc. stay registered until Phase 5 retires
// them; agents picking up the new tools first get the cleaner shape.

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/christophermarx/agentboard/internal/store"
)

// v2ToolDefs returns the new tier-shaped tool definitions. Caller folds
// these into toolDefinitions().
func (s *Server) v2ToolDefs() []ToolDef {
	if s.FileStore == nil {
		return nil
	}
	return []ToolDef{
		{
			Name:        "agentboard_v2_index",
			Description: "Tier 1 — return the catalog of every key in the project: shape, version, size. One call after wakeup to orient.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "agentboard_v2_search",
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
			Name:        "agentboard_v2_read",
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
			Name:        "agentboard_v2_write",
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
			Name:        "agentboard_v2_merge",
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
			Name:        "agentboard_v2_append",
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
		{
			Name:        "agentboard_v2_increment",
			Description: "Atomically add to a numeric singleton. Default `by` is 1. Conflict-free.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]string{"type": "string", "description": "Dotted key"},
					"by":  map[string]string{"type": "number", "description": "Delta (positive or negative); default 1"},
				},
				"required": []string{"key"},
			},
		},
		{
			Name:        "agentboard_v2_cas",
			Description: "Atomic test-and-set. Succeeds iff current value deeply equals `expected`; returns 409 with current value on mismatch.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":      map[string]string{"type": "string", "description": "Dotted key"},
					"id":       map[string]string{"type": "string", "description": "Item ID (omit for singleton)"},
					"expected": map[string]string{"description": "JSON value the current state must equal"},
					"new":      map[string]string{"description": "JSON value to write on match"},
				},
				"required": []string{"key", "expected", "new"},
			},
		},
		{
			Name:        "agentboard_v2_delete",
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
			Name:        "agentboard_v2_history",
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
			Name:        "agentboard_v2_activity",
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

// dispatchV2 routes the agentboard_v2_* tools. Returns (result, ok). ok
// is false when the tool name didn't match — caller falls through to
// the legacy switch.
func (s *Server) dispatchV2(name string, args map[string]json.RawMessage) (any, *RPCError, bool) {
	if s.FileStore == nil {
		return nil, nil, false
	}
	switch name {
	case "agentboard_v2_index":
		return s.toolV2Index()
	case "agentboard_v2_search":
		return s.toolV2Search(args)
	case "agentboard_v2_read":
		return s.toolV2Read(args)
	case "agentboard_v2_write":
		return s.toolV2Write(args)
	case "agentboard_v2_merge":
		return s.toolV2Merge(args)
	case "agentboard_v2_append":
		return s.toolV2Append(args)
	case "agentboard_v2_increment":
		return s.toolV2Increment(args)
	case "agentboard_v2_cas":
		return s.toolV2CAS(args)
	case "agentboard_v2_delete":
		return s.toolV2Delete(args)
	case "agentboard_v2_history":
		return s.toolV2History(args)
	case "agentboard_v2_activity":
		return s.toolV2Activity(args)
	}
	return nil, nil, false
}

// dispatchV2 wrappers. Each tool returns the result body wrapped in
// MCP content; structured store errors translate to RPCError with
// embedded current state where applicable.

func (s *Server) toolV2Index() (any, *RPCError, bool) {
	cat := s.FileStore.Catalog()
	return mcpJSON(map[string]any{"data": cat, "count": len(cat)}), nil, true
}

func (s *Server) toolV2Search(args map[string]json.RawMessage) (any, *RPCError, bool) {
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

func (s *Server) toolV2Read(args map[string]json.RawMessage) (any, *RPCError, bool) {
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

func (s *Server) toolV2Write(args map[string]json.RawMessage) (any, *RPCError, bool) {
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

func (s *Server) toolV2Merge(args map[string]json.RawMessage) (any, *RPCError, bool) {
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

func (s *Server) toolV2Append(args map[string]json.RawMessage) (any, *RPCError, bool) {
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

func (s *Server) toolV2Increment(args map[string]json.RawMessage) (any, *RPCError, bool) {
	key := getString(args, "key")
	if key == "" {
		return nil, &RPCError{Code: -32602, Message: "key required"}, true
	}
	by := 1.0
	if raw, ok := args["by"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &by); err != nil {
			return nil, &RPCError{Code: -32602, Message: "`by` must be a number"}, true
		}
	}
	env, err := s.FileStore.Increment(key, by, s.actor())
	if err != nil {
		return nil, storeRPCErr(err), true
	}
	return mcpJSON(env), nil, true
}

func (s *Server) toolV2CAS(args map[string]json.RawMessage) (any, *RPCError, bool) {
	key := getString(args, "key")
	if key == "" {
		return nil, &RPCError{Code: -32602, Message: "key required"}, true
	}
	expected := args["expected"]
	next := args["new"]
	if len(expected) == 0 || len(next) == 0 {
		return nil, &RPCError{Code: -32602, Message: "cas requires both `expected` and `new`"}, true
	}
	id := getString(args, "id")
	actor := s.actor()

	var env *store.Envelope
	var err error
	if id != "" {
		env, err = s.FileStore.CASItem(key, id, expected, next, actor)
	} else {
		env, err = s.FileStore.CAS(key, expected, next, actor)
	}
	if err != nil {
		return nil, storeRPCErr(err), true
	}
	return mcpJSON(env), nil, true
}

func (s *Server) toolV2Delete(args map[string]json.RawMessage) (any, *RPCError, bool) {
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

func (s *Server) toolV2History(args map[string]json.RawMessage) (any, *RPCError, bool) {
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

func (s *Server) toolV2Activity(args map[string]json.RawMessage) (any, *RPCError, bool) {
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
