package mcp

// Cut 6 handlers. Each tool dispatches by path through one of the
// content-tier subsystems:
//
//   - PageManager (pages, including singletons that live as `<key>.md`
//     in the page tree)
//   - Store        (data tier — singletons, collections, streams)
//   - Files        (binary uploads via presigned URL)
//   - Grab         (cross-page materializer)
//   - WebhookDispatcher (fire_event)
//
// A path is resolved against the page tree first; if that misses, the
// data catalog is consulted. New writes default to the page tree
// when `body` is non-empty, otherwise to the data tier (singletons
// without body land alongside other store data so the catalog/index
// stays unified). All paths use forward slashes; no leading or
// trailing slash; no `.md` suffix.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/grab"
	"github.com/christophermarx/agentboard/internal/store"
	"github.com/christophermarx/agentboard/internal/webhooks"
)

// ---------- agentboard_read ----------

type readResult struct {
	Path        string         `json:"path"`
	Success     bool           `json:"success"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Body        string         `json:"body,omitempty"`
	Version     string         `json:"version,omitempty"`
	Shape       string         `json:"shape,omitempty"`
	Items       []any          `json:"items,omitempty"` // collections
	Lines       []any          `json:"lines,omitempty"` // streams
	Error       *toolError     `json:"error,omitempty"`
}

func (s *Server) toolRead(_ *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	var paths []string
	if raw, ok := args["paths"]; ok {
		if err := json.Unmarshal(raw, &paths); err != nil {
			return nil, &RPCError{Code: -32602, Message: "paths must be an array of strings"}
		}
	}
	if len(paths) == 0 {
		return nil, &RPCError{Code: -32602, Message: "paths required (always-plural batch)"}
	}

	results := make([]readResult, 0, len(paths))
	allOk := true
	for _, p := range paths {
		results = append(results, s.readOne(p))
		if !results[len(results)-1].Success {
			allOk = false
		}
	}
	return mcpJSON(map[string]any{"results": results, "all_succeeded": allOk}), nil
}

func (s *Server) readOne(path string) readResult {
	norm := normalizeLeafPath(path)
	if norm == "" {
		return readResult{Path: path, Success: false, Error: &toolError{Code: "invalid_path", Message: "path required"}}
	}

	// 1) Page tree.
	if s.Pages != nil {
		if p := s.Pages.GetPage(norm); p != nil {
			return readResult{
				Path:        norm,
				Success:     true,
				Frontmatter: p.Frontmatter,
				Body:        p.Source,
				Version:     p.Version,
				Shape:       "page",
			}
		}
	}

	// 2) Data tier.
	if s.FileStore != nil {
		if cat, ok := s.FileStore.CatalogGet(norm); ok {
			switch cat.Shape {
			case store.ShapeSingleton:
				env, err := s.FileStore.ReadSingleton(norm)
				if err != nil {
					return readResult{Path: norm, Success: false, Error: storeToolErr(err)}
				}
				return envelopeToReadResult(norm, env, store.ShapeSingleton)
			case store.ShapeCollection:
				items, err := s.FileStore.ListCollection(norm)
				if err != nil {
					return readResult{Path: norm, Success: false, Error: storeToolErr(err)}
				}
				out := make([]any, 0, len(items))
				for _, it := range items {
					var fm map[string]any
					if it.Envelope != nil {
						_ = json.Unmarshal(it.Envelope.Value, &fm)
					}
					out = append(out, map[string]any{
						"id":          it.ID,
						"frontmatter": fm,
						"version":     it.Envelope.Meta.Version,
					})
				}
				return readResult{Path: norm, Success: true, Shape: "collection", Items: out}
			case store.ShapeStream:
				lines, err := s.FileStore.ReadStream(norm, store.ReadStreamOpts{Limit: 100})
				if err != nil {
					return readResult{Path: norm, Success: false, Error: storeToolErr(err)}
				}
				out := make([]any, 0, len(lines))
				for _, l := range lines {
					var v any
					_ = json.Unmarshal(l.Value, &v)
					out = append(out, v)
				}
				return readResult{Path: norm, Success: true, Shape: "stream", Lines: out}
			}
		}
	}

	return readResult{Path: norm, Success: false, Error: &toolError{Code: "not_found", Message: "no leaf at this path"}}
}

func envelopeToReadResult(path string, env *store.Envelope, shape string) readResult {
	if env == nil {
		return readResult{Path: path, Success: false, Error: &toolError{Code: "not_found", Message: "no value"}}
	}
	var fm map[string]any
	_ = json.Unmarshal(env.Value, &fm)
	return readResult{
		Path:        path,
		Success:     true,
		Frontmatter: fm,
		Body:        env.Body,
		Version:     env.Meta.Version,
		Shape:       shape,
	}
}

// ---------- agentboard_list ----------

func (s *Server) toolList(_ *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	path := getString(args, "path")
	norm := normalizeLeafPath(path)

	out := []map[string]any{}

	// Page-tree children: pages whose path is a direct child of `norm/`.
	if s.Pages != nil {
		prefix := norm
		if prefix != "" {
			prefix = prefix + "/"
		}
		for _, p := range s.Pages.ListPages() {
			pp := strings.TrimPrefix(p.Path, "/")
			if pp == "index" {
				continue
			}
			if !strings.HasPrefix(pp, prefix) {
				continue
			}
			rest := pp[len(prefix):]
			if rest == "" || strings.Contains(rest, "/") {
				continue
			}
			out = append(out, map[string]any{
				"path":        pp,
				"title":       p.Title,
				"frontmatter": p.Frontmatter,
				"version":     p.Version,
				"shape":       "page",
			})
		}
	}

	// Data-tier children: catalog entries that look like `norm/<id>` or
	// the entry at `norm` itself (collection key).
	if s.FileStore != nil {
		for _, e := range s.FileStore.Catalog() {
			if e.Key == norm && e.Shape == store.ShapeCollection {
				items, err := s.FileStore.ListCollection(e.Key)
				if err != nil {
					continue
				}
				for _, it := range items {
					var fm map[string]any
					if it.Envelope != nil {
						_ = json.Unmarshal(it.Envelope.Value, &fm)
					}
					out = append(out, map[string]any{
						"path":        norm + "/" + it.ID,
						"id":          it.ID,
						"frontmatter": fm,
						"version":     it.Envelope.Meta.Version,
						"shape":       "collection_item",
					})
				}
			}
		}
	}

	return mcpJSON(map[string]any{
		"path":     norm,
		"children": out,
		"count":    len(out),
	}), nil
}

// ---------- agentboard_search ----------

func (s *Server) toolSearch(_ *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	q := getString(args, "q")
	if q == "" {
		return nil, &RPCError{Code: -32602, Message: "q is required"}
	}
	scope := getString(args, "scope")
	if scope == "" {
		scope = "all"
	}
	limit := 20
	if raw, ok := args["limit"]; ok {
		var n int
		if err := json.Unmarshal(raw, &n); err == nil && n > 0 {
			limit = n
		}
	}

	type hit struct {
		Path    string  `json:"path"`
		Title   string  `json:"title,omitempty"`
		Snippet string  `json:"snippet,omitempty"`
		Score   float64 `json:"score,omitempty"`
		Shape   string  `json:"shape,omitempty"`
	}
	hits := []hit{}

	// Pages — FTS5.
	if (scope == "all" || scope == "pages") && s.FileStore != nil && s.Pages != nil {
		// Pages live under the page manager's index; use the search
		// store wired into the file store via the embedded store
		// package. We expose a Search method on Store for data; for
		// pages we fall back to substring across PageInfo.Source +
		// title here so this tool works without the FTS index being
		// available.
		needle := strings.ToLower(q)
		for _, p := range s.Pages.ListPages() {
			haystack := strings.ToLower(p.Title + "\n" + p.Source)
			if strings.Contains(haystack, needle) {
				hits = append(hits, hit{
					Path:  strings.TrimPrefix(p.Path, "/"),
					Title: p.Title,
					Shape: "page",
				})
				if len(hits) >= limit {
					break
				}
			}
		}
	}

	// Data — substring score.
	if (scope == "all" || scope == "data") && s.FileStore != nil && len(hits) < limit {
		results, err := s.FileStore.Search(store.SearchOpts{Query: q, Limit: limit - len(hits)})
		if err == nil {
			for _, r := range results {
				path := r.Key
				if r.ID != "" {
					path = r.Key + "/" + r.ID
				}
				hits = append(hits, hit{
					Path:    path,
					Snippet: r.Snippet,
					Score:   r.Score,
					Shape:   r.Shape,
				})
			}
		}
	}

	return mcpJSON(map[string]any{"query": q, "scope": scope, "hits": hits}), nil
}

// ---------- agentboard_write ----------

type writeItem struct {
	Path        string         `json:"path"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Body        *string        `json:"body,omitempty"`
	Version     string         `json:"version,omitempty"`
}

type writeResult struct {
	Path     string          `json:"path"`
	Success  bool            `json:"success"`
	Version  string          `json:"version,omitempty"`
	Warnings []store.Warning `json:"warnings,omitempty"`
	Error    *toolError      `json:"error,omitempty"`
}

func (s *Server) toolWrite(r *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	var items []writeItem
	if raw, ok := args["items"]; ok {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, &RPCError{Code: -32602, Message: "items must be an array of {path, frontmatter?, body?, version?}: " + err.Error()}
		}
	}
	if len(items) == 0 {
		return nil, &RPCError{Code: -32602, Message: "items required (always-plural batch)"}
	}

	actor := s.resolveActor(r)
	results := make([]writeResult, 0, len(items))
	allOk := true
	for _, it := range items {
		results = append(results, s.writeOne(it, actor))
		if !results[len(results)-1].Success {
			allOk = false
		}
	}
	return mcpJSON(map[string]any{"results": results, "all_succeeded": allOk}), nil
}

func (s *Server) writeOne(it writeItem, actor string) writeResult {
	norm := normalizeLeafPath(it.Path)
	if norm == "" {
		return writeResult{Path: it.Path, Success: false, Error: &toolError{Code: "invalid_path", Message: "path required"}}
	}

	// Routing rules:
	//   1. Existing page → page tier.
	//   2. Existing data leaf (singleton / collection / collection-item /
	//      stream) → data tier.
	//   3. Brand-new write:
	//      - Path contains a slash → page tier (data store only accepts
	//        flat keys; nested layouts live in the page tree).
	//      - Body supplied → page tier (data leaves don't carry body).
	//      - Otherwise → data tier as a flat-key singleton.
	//
	// This keeps existing data writes working (no churn on
	// `dev.features.foo.bar`) while honouring spec §1's "one tree with
	// arbitrary nesting" via the page tier.
	hasBody := it.Body != nil
	existingPage := s.Pages != nil && s.Pages.GetPage(norm) != nil
	existingDataLeaf := false
	if s.FileStore != nil {
		if _, ok := s.FileStore.CatalogGet(norm); ok {
			existingDataLeaf = true
		} else if _, ok := splitCollectionPath(s.FileStore, norm); ok {
			existingDataLeaf = true
		}
	}

	target := "page"
	switch {
	case existingPage:
		target = "page"
	case existingDataLeaf:
		target = "data"
	case strings.Contains(norm, "/"):
		target = "page"
	case hasBody:
		target = "page"
	default:
		target = "data"
	}

	if target == "page" {
		return s.writePage(norm, it, actor)
	}
	return s.writeData(norm, it, actor)
}

func (s *Server) writePage(path string, it writeItem, actor string) writeResult {
	if s.Pages == nil {
		return writeResult{Path: path, Success: false, Error: &toolError{Code: "page_unavailable", Message: "page manager not configured"}}
	}
	body := ""
	if it.Body != nil {
		body = *it.Body
	}
	fm := map[string]any{}
	for k, v := range it.Frontmatter {
		fm[k] = v
	}
	source, err := store.AssemblePageSource(fm, body)
	if err != nil {
		return writeResult{Path: path, Success: false, Error: &toolError{Code: "assemble", Message: err.Error()}}
	}
	if err := s.Pages.WritePageIfMatch(path, source, it.Version); err != nil {
		return writeResult{Path: path, Success: false, Error: pageToolErr(err)}
	}
	// Run the same post-write hooks the REST handler runs (PageMeta,
	// PageRefs, Search, mention dispatch, SSE broadcast). The file
	// watcher would also pick up the change on its 500 ms debounce,
	// but rapid batch writes through MCP can race with directory-
	// create events so we run the hooks synchronously here as the
	// canonical path. The HTTP server injects AfterPageWrite at boot
	// (cli/serve.go); tests can leave it nil.
	if s.AfterPageWrite != nil {
		s.AfterPageWrite(path, source, actor)
	}
	page := s.Pages.GetPage(path)
	res := writeResult{Path: path, Success: true}
	if page != nil {
		res.Version = page.Version
		res.Warnings = store.CheckShape(path, page.Frontmatter)
	}
	return res
}

func (s *Server) writeData(path string, it writeItem, actor string) writeResult {
	if s.FileStore == nil {
		return writeResult{Path: path, Success: false, Error: &toolError{Code: "data_unavailable", Message: "store not configured"}}
	}
	// Frontmatter-only write → marshal frontmatter as the value blob.
	// Spec §6 + §3: data singletons are .md files with frontmatter
	// only. We round-trip through json.RawMessage so the value travels
	// as native JSON (no double-stringify per Issue 1).
	value := json.RawMessage("null")
	if len(it.Frontmatter) > 0 {
		raw, err := json.Marshal(it.Frontmatter)
		if err != nil {
			return writeResult{Path: path, Success: false, Error: &toolError{Code: "encode", Message: err.Error()}}
		}
		value = raw
	}

	// Detect existing collection/item shape: a path with `<key>/<id>`
	// where <key> exists as a collection routes to UpsertItem.
	if cat, ok := splitCollectionPath(s.FileStore, path); ok {
		env, err := s.FileStore.UpsertItem(cat.key, cat.id, value, it.Version, actor)
		if err != nil {
			return writeResult{Path: path, Success: false, Error: storeToolErr(err)}
		}
		return finalizeWriteResult(path, env, it.Frontmatter)
	}

	env, err := s.FileStore.Set(path, value, it.Version, actor)
	if err != nil {
		return writeResult{Path: path, Success: false, Error: storeToolErr(err)}
	}
	return finalizeWriteResult(path, env, it.Frontmatter)
}

func finalizeWriteResult(path string, env *store.Envelope, frontmatter map[string]any) writeResult {
	res := writeResult{Path: path, Success: true}
	if env != nil {
		res.Version = env.Meta.Version
	}
	res.Warnings = store.CheckShape(path, frontmatter)
	return res
}

// ---------- agentboard_patch ----------

type patchItem struct {
	Path             string         `json:"path"`
	FrontmatterPatch map[string]any `json:"frontmatter_patch,omitempty"`
	Body             *string        `json:"body,omitempty"`
	Version          string         `json:"version,omitempty"`
}

func (s *Server) toolPatch(r *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	var items []patchItem
	if raw, ok := args["items"]; ok {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, &RPCError{Code: -32602, Message: "items must be an array of {path, frontmatter_patch?, body?, version?}: " + err.Error()}
		}
	}
	if len(items) == 0 {
		return nil, &RPCError{Code: -32602, Message: "items required (always-plural batch)"}
	}
	actor := s.resolveActor(r)

	results := make([]writeResult, 0, len(items))
	allOk := true
	for _, it := range items {
		results = append(results, s.patchOne(it, actor))
		if !results[len(results)-1].Success {
			allOk = false
		}
	}
	return mcpJSON(map[string]any{"results": results, "all_succeeded": allOk}), nil
}

func (s *Server) patchOne(it patchItem, actor string) writeResult {
	norm := normalizeLeafPath(it.Path)
	if norm == "" {
		return writeResult{Path: it.Path, Success: false, Error: &toolError{Code: "invalid_path", Message: "path required"}}
	}
	if it.FrontmatterPatch == nil && it.Body == nil {
		return writeResult{Path: norm, Success: false, Error: &toolError{Code: "invalid_value", Message: "frontmatter_patch and/or body required"}}
	}

	// Page tier — current page must exist (we can't patch what's not
	// there). Body is preserved when nil.
	if s.Pages != nil {
		if cur := s.Pages.GetPage(norm); cur != nil {
			merged := map[string]any{}
			for k, v := range cur.Frontmatter {
				merged[k] = v
			}
			for k, v := range it.FrontmatterPatch {
				if v == nil {
					delete(merged, k)
				} else {
					merged[k] = v
				}
			}
			body := cur.Source
			if it.Body != nil {
				body = *it.Body
			}
			source, err := store.AssemblePageSource(merged, body)
			if err != nil {
				return writeResult{Path: norm, Success: false, Error: &toolError{Code: "assemble", Message: err.Error()}}
			}
			if err := s.Pages.WritePageIfMatch(norm, source, it.Version); err != nil {
				return writeResult{Path: norm, Success: false, Error: pageToolErr(err)}
			}
			if s.AfterPageWrite != nil {
				s.AfterPageWrite(norm, source, actor)
			}
			page := s.Pages.GetPage(norm)
			res := writeResult{Path: norm, Success: true}
			if page != nil {
				res.Version = page.Version
				res.Warnings = store.CheckShape(norm, page.Frontmatter)
			}
			return res
		}
	}

	// Data tier — RFC-7396 merge against the singleton/collection-item.
	if s.FileStore != nil {
		patchBytes, err := json.Marshal(it.FrontmatterPatch)
		if err != nil {
			return writeResult{Path: norm, Success: false, Error: &toolError{Code: "encode", Message: err.Error()}}
		}
		if cat, ok := splitCollectionPath(s.FileStore, norm); ok {
			env, err := s.FileStore.MergeItem(cat.key, cat.id, patchBytes, actor)
			if err != nil {
				return writeResult{Path: norm, Success: false, Error: storeToolErr(err)}
			}
			return finalizeWriteResult(norm, env, it.FrontmatterPatch)
		}
		env, err := s.FileStore.Merge(norm, patchBytes, actor)
		if err != nil {
			return writeResult{Path: norm, Success: false, Error: storeToolErr(err)}
		}
		return finalizeWriteResult(norm, env, it.FrontmatterPatch)
	}

	return writeResult{Path: norm, Success: false, Error: &toolError{Code: "not_found", Message: "no leaf at this path"}}
}

// ---------- agentboard_append ----------

func (s *Server) toolAppend(r *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	if s.FileStore == nil {
		return nil, &RPCError{Code: -32000, Message: "store not configured"}
	}
	path := getString(args, "path")
	norm := normalizeLeafPath(path)
	if norm == "" {
		return nil, &RPCError{Code: -32602, Message: "path required"}
	}
	var items []json.RawMessage
	if raw, ok := args["items"]; ok {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, &RPCError{Code: -32602, Message: "items must be a JSON array"}
		}
	}
	if len(items) == 0 {
		return nil, &RPCError{Code: -32602, Message: "items required (always-plural batch)"}
	}
	actor := s.resolveActor(r)
	lines, err := s.FileStore.AppendBatch(norm, items, actor)
	if err != nil {
		return mcpJSON(map[string]any{
			"path":          norm,
			"appended":      0,
			"error":         storeToolErr(err),
			"all_succeeded": false,
		}), nil
	}
	return mcpJSON(map[string]any{
		"path":          norm,
		"appended":      len(lines),
		"lines":         lines,
		"all_succeeded": true,
	}), nil
}

// ---------- agentboard_delete ----------

type deleteItem struct {
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
}

type deleteResult struct {
	Path    string     `json:"path"`
	Success bool       `json:"success"`
	Error   *toolError `json:"error,omitempty"`
}

func (s *Server) toolDelete(r *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	var items []deleteItem
	if raw, ok := args["items"]; ok {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, &RPCError{Code: -32602, Message: "items must be an array of {path, version?}: " + err.Error()}
		}
	}
	if len(items) == 0 {
		return nil, &RPCError{Code: -32602, Message: "items required (always-plural batch)"}
	}
	actor := s.resolveActor(r)

	results := make([]deleteResult, 0, len(items))
	allOk := true
	for _, it := range items {
		res := s.deleteOne(it, actor)
		results = append(results, res)
		if !res.Success {
			allOk = false
		}
	}
	return mcpJSON(map[string]any{"results": results, "all_succeeded": allOk}), nil
}

func (s *Server) deleteOne(it deleteItem, actor string) deleteResult {
	norm := normalizeLeafPath(it.Path)
	if norm == "" {
		return deleteResult{Path: it.Path, Success: false, Error: &toolError{Code: "invalid_path", Message: "path required"}}
	}

	// Page tier — present means delete it.
	if s.Pages != nil {
		if s.Pages.GetPage(norm) != nil {
			if err := s.Pages.DeletePageIfMatch(norm, it.Version); err != nil {
				return deleteResult{Path: norm, Success: false, Error: pageToolErr(err)}
			}
			return deleteResult{Path: norm, Success: true}
		}
	}

	// Data tier — singleton / item / collection / stream.
	if s.FileStore != nil {
		if cat, ok := splitCollectionPath(s.FileStore, norm); ok {
			version := it.Version
			if version == "" {
				version = "*"
			}
			if err := s.FileStore.DeleteItem(cat.key, cat.id, version, actor); err != nil {
				return deleteResult{Path: norm, Success: false, Error: storeToolErr(err)}
			}
			return deleteResult{Path: norm, Success: true}
		}
		if cat, ok := s.FileStore.CatalogGet(norm); ok {
			version := it.Version
			if version == "" {
				version = "*"
			}
			var err error
			switch cat.Shape {
			case store.ShapeSingleton:
				err = s.FileStore.DeleteSingleton(norm, version, actor)
			case store.ShapeStream:
				err = s.FileStore.DeleteStream(norm, actor)
			case store.ShapeCollection:
				err = s.FileStore.DeleteCollection(norm, actor)
			}
			if err != nil {
				return deleteResult{Path: norm, Success: false, Error: storeToolErr(err)}
			}
			return deleteResult{Path: norm, Success: true}
		}
	}

	// Idempotent: no leaf at this path is treated as success.
	return deleteResult{Path: norm, Success: true}
}

// ---------- agentboard_request_file_upload ----------

type uploadItem struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

type uploadResult struct {
	Name         string     `json:"name"`
	Success      bool       `json:"success"`
	UploadURL    string     `json:"upload_url,omitempty"`
	ExpiresAt    string     `json:"expires_at,omitempty"`
	MaxSizeBytes int64      `json:"max_size_bytes,omitempty"`
	Hint         string     `json:"hint,omitempty"`
	Error        *toolError `json:"error,omitempty"`
}

func (s *Server) toolRequestFileUpload(r *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	if s.MintUploadToken == nil {
		return nil, &RPCError{Code: -32000, Message: "upload subsystem not configured"}
	}
	var items []uploadItem
	if raw, ok := args["items"]; ok {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, &RPCError{Code: -32602, Message: "items must be an array of {name, size_bytes?}: " + err.Error()}
		}
	}
	if len(items) == 0 {
		return nil, &RPCError{Code: -32602, Message: "items required (always-plural batch)"}
	}
	actor := s.resolveActor(r)

	results := make([]uploadResult, 0, len(items))
	allOk := true
	for _, it := range items {
		if it.Name == "" {
			results = append(results, uploadResult{Name: it.Name, Success: false, Error: &toolError{Code: "invalid_name", Message: "name required"}})
			allOk = false
			continue
		}
		url, expiresAt, maxBytes, ok := s.MintUploadToken(it.Name, actor, it.SizeBytes)
		if !ok {
			results = append(results, uploadResult{Name: it.Name, Success: false, Error: &toolError{Code: "mint_failed", Message: "could not mint token (check size cap or filename rules)"}})
			allOk = false
			continue
		}
		results = append(results, uploadResult{
			Name:         it.Name,
			Success:      true,
			UploadURL:    url,
			ExpiresAt:    expiresAt,
			MaxSizeBytes: maxBytes,
			Hint:         "Shell out: curl -X PUT --data-binary @<path> <upload_url>",
		})
	}
	return mcpJSON(map[string]any{"results": results, "all_succeeded": allOk}), nil
}

// ---------- agentboard_grab ----------

func (s *Server) toolGrab(_ *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	if s.Grab == nil {
		return nil, &RPCError{Code: -32000, Message: "grab materializer not configured"}
	}
	var picks []grab.Pick
	if raw, ok := args["picks"]; ok {
		if err := json.Unmarshal(raw, &picks); err != nil {
			return nil, &RPCError{Code: -32602, Message: "picks must be an array: " + err.Error()}
		}
	}
	if len(picks) == 0 {
		return nil, &RPCError{Code: -32602, Message: "at least one pick is required"}
	}
	format := grab.Format(getString(args, "format"))
	if format != grab.FormatXML && format != grab.FormatJSON {
		format = grab.FormatMarkdown
	}
	sections := s.Grab.Materialize(picks)
	text := grab.Render(sections, format)
	return mcpContent(text), nil
}

// ---------- agentboard_fire_event ----------

func (s *Server) toolFireEvent(r *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
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
	if actor := s.resolveActor(r); actor != "" {
		payload["fired_by"] = actor
	}
	s.WebhookDispatcher.Emit(webhooks.Event{Name: eventName, Data: payload})
	return mcpJSON(map[string]any{"ok": true, "event": eventName}), nil
}

// ---------- helpers ----------

// toolError is the structured per-item error in batch responses.
type toolError struct {
	Code           string          `json:"code"`
	Message        string          `json:"message"`
	Current        *store.Envelope `json:"current,omitempty"`
	YourVersion    string          `json:"your_version,omitempty"`
	CurrentVersion string          `json:"current_version,omitempty"`
}

// normalizeLeafPath strips a leading slash + `.md` suffix and rejects
// the obvious traversal cases. Empty input returns "" so callers can
// distinguish "no path" from a normalized one.
//
// Any segment beginning with a dot is rejected — this is the dotfile
// blocklist that keeps user-supplied paths out of `.agentboard/`,
// `.git/`, `.trash/`, and friends. It also catches `..` traversal as
// a side effect.
func normalizeLeafPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, ".md")
	if p == "" {
		return ""
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return ""
		}
	}
	return p
}

// splitCollectionPath inspects a `<key>/<id>` shape. Returns ok=true
// only when `<key>` is a known collection in the catalog; that's the
// signal we should route to UpsertItem / MergeItem / DeleteItem
// instead of treating the slash as part of the key.
type collectionRef struct{ key, id string }

func splitCollectionPath(s *store.Store, path string) (collectionRef, bool) {
	if s == nil {
		return collectionRef{}, false
	}
	idx := strings.LastIndex(path, "/")
	if idx <= 0 || idx == len(path)-1 {
		return collectionRef{}, false
	}
	key := path[:idx]
	id := path[idx+1:]
	cat, ok := s.CatalogGet(key)
	if !ok || cat.Shape != store.ShapeCollection {
		return collectionRef{}, false
	}
	return collectionRef{key: key, id: id}, true
}

// pageToolErr translates page-write sentinels into the per-item
// toolError shape used in batch responses.
func pageToolErr(err error) *toolError {
	switch {
	case errors.Is(err, store.ErrPageStale):
		return &toolError{Code: "version_mismatch", Message: err.Error()}
	case errors.Is(err, store.ErrPageNotFoundForMatch):
		return &toolError{Code: "not_found", Message: err.Error()}
	}
	return &toolError{Code: "internal", Message: err.Error()}
}

// storeToolErr translates store sentinels into per-item toolError
// values. Conflict + CAS responses inline the current envelope so an
// agent can reconcile in one round-trip.
func storeToolErr(err error) *toolError {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return &toolError{Code: "not_found", Message: "no value at this path"}
	case errors.Is(err, store.ErrVersionRequired):
		return &toolError{Code: "version_required", Message: "include `version` from a prior read, or pass '*' to force"}
	case errors.Is(err, store.ErrLineTooLong):
		return &toolError{Code: "line_too_long", Message: "stream lines must be ≤ 4096 bytes"}
	case errors.Is(err, store.ErrInvalidValue):
		return &toolError{Code: "invalid_value", Message: err.Error()}
	}
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		te := &toolError{Code: "version_mismatch", Message: "this leaf was modified since your read; reconcile + retry", Current: conflict.Current, YourVersion: conflict.YourVersion}
		if conflict.Current != nil {
			te.CurrentVersion = conflict.Current.Meta.Version
		}
		return te
	}
	var cas *store.CASError
	if errors.As(err, &cas) {
		return &toolError{Code: "cas_mismatch", Message: "expected value did not equal current", Current: cas.Current}
	}
	var ws *store.WrongShapeError
	if errors.As(err, &ws) {
		return &toolError{Code: "wrong_shape", Message: fmt.Sprintf("this path is %s; tried to write as %s — pick a new path", ws.Actual, ws.Attempt)}
	}
	return &toolError{Code: "internal", Message: err.Error()}
}
