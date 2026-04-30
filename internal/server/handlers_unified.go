package server

// Cut 7: REST namespace unification. Spec §5 says one namespace —
// `/api/<path>` GET/PUT/PATCH/DELETE/POST :append — covers the entire
// content tier. The dispatcher routes by lookup: page tree first
// (handles arbitrary nesting), then the data catalog. New writes go
// to the page tree when `body` is present or the path has a slash;
// otherwise to the data tier as a flat-key singleton. Same routing
// rules the MCP layer uses (internal/mcp/handlers.go::writeOne).
//
// Reserved /api/* prefixes (admin, auth, view, files, components, etc.)
// are registered before this catch-all wildcard so they win the chi
// dispatcher; everything else is content.
//
// The legacy `/api/content/*` and `/api/data/{key}` routes call into
// the same underlying store/page-manager primitives, so a path
// reachable via the unified namespace also writes the same bytes
// regardless of which surface the agent picks. The next cut retires
// the legacy routes once SPA + tests + bruno have all migrated.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/locks"
	"github.com/christophermarx/agentboard/internal/store"
	"github.com/go-chi/chi/v5"
)

// extractUnifiedPath pulls the leaf path from a chi catch-all route.
// Strips a trailing `:append` verb so the caller can route POST
// `/api/<path>:append` to the stream-append path. Applies the
// SKILL.md → folder collapse via store.NormalizePagePath so
// `/api/skills/<slug>/SKILL.md` resolves to the skill's folder
// index page (spec §1 path layout).
func extractUnifiedPath(r *http.Request) (path string, isAppend bool) {
	raw := chi.URLParam(r, "*")
	if strings.HasSuffix(raw, ":append") {
		return store.NormalizePagePath(strings.TrimSuffix(raw, ":append")), true
	}
	return store.NormalizePagePath(raw), false
}

// handleUnifiedRead is GET /api/<path>. Returns the page envelope for
// a page leaf, the singleton/collection/stream payload for a data
// leaf, 404 for anything else. Per spec §5 + §6 every read returns
// the same envelope shape: {frontmatter, body, version}. Recognizes
// the `/history` suffix per spec §5 (`GET /api/<path>/history`) and
// routes to the per-doc audit log.
func (s *Server) handleUnifiedRead(w http.ResponseWriter, r *http.Request) {
	rawPath := chi.URLParam(r, "*")
	if strings.HasSuffix(rawPath, "/history") {
		s.handleUnifiedHistory(w, r, store.NormalizePagePath(strings.TrimSuffix(rawPath, "/history")))
		return
	}
	path, _ := extractUnifiedPath(r)
	if path == "" {
		path = "index"
	}

	// Page tier first.
	if s.Pages != nil {
		if page := s.Pages.GetPage(path); page != nil {
			s.respondUnifiedPage(w, r, page)
			return
		}
	}

	// Data tier — singleton, collection, or stream.
	if s.FileStore != nil {
		if cat, ok := s.FileStore.CatalogGet(path); ok {
			switch cat.Shape {
			case store.ShapeSingleton:
				env, err := s.FileStore.ReadSingleton(path)
				if err != nil {
					translateStoreError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, env)
				return
			case store.ShapeCollection:
				items, err := s.FileStore.ListCollection(path)
				if err != nil {
					translateStoreError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"_meta": map[string]any{"shape": store.ShapeCollection, "key": path, "count": len(items)},
					"items": items,
				})
				return
			case store.ShapeStream:
				lines, err := s.FileStore.ReadStream(path, store.ReadStreamOpts{Limit: 100})
				if err != nil {
					translateStoreError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"_meta": map[string]any{"shape": store.ShapeStream, "key": path, "line_count": len(lines)},
					"lines": lines,
				})
				return
			}
		}
		// Collection-item read: "key/id" where key is a known collection.
		if key, id, ok := splitCollectionAddress(s.FileStore, path); ok {
			env, err := s.FileStore.ReadItem(key, id)
			if err != nil {
				translateStoreError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, env)
			return
		}
	}

	respondError(w, http.StatusNotFound, "NOT_FOUND", "no leaf at "+path)
}

// respondUnifiedPage is the page-tier read response. Same shape the
// legacy handleGetPage emitted, modulo the Accept: text/markdown
// branch which still works.
func (s *Server) respondUnifiedPage(w http.ResponseWriter, r *http.Request, page *store.PageInfo) {
	if page.Version != "" {
		w.Header().Set("ETag", `"`+page.Version+`"`)
	}
	pagePath := strings.TrimPrefix(page.Path, "/")
	if pagePath == "" {
		pagePath = "index"
	}

	var meta *store.PageMeta
	if s.PageMeta != nil {
		meta, _ = s.PageMeta.Get(pagePath)
		if meta != nil {
			w.Header().Set("X-Last-Actor", meta.LastActor)
			w.Header().Set("X-Last-At", meta.LastAt)
		}
	}
	var approval *store.PageApproval
	var approvalStale bool
	if s.PageApproval != nil {
		approval, _ = s.PageApproval.Get(pagePath)
		if approval != nil {
			approvalStale = approval.ApprovedEtag != page.Version
			w.Header().Set("X-Approved-By", approval.ApprovedBy)
			w.Header().Set("X-Approved-At", approval.ApprovedAt)
			w.Header().Set("X-Approved-Etag", approval.ApprovedEtag)
			if approvalStale {
				w.Header().Set("X-Approval-Stale", "true")
			}
		}
	}
	var lockRow *locks.Lock
	if s.Locks != nil {
		lockRow, _ = s.Locks.Get(pagePath)
	}
	if lockRow != nil {
		w.Header().Set("X-Locked-By", lockRow.LockedBy)
		w.Header().Set("X-Locked-At", lockRow.LockedAt.Format(time.RFC3339))
		if lockRow.Reason != "" {
			w.Header().Set("X-Locked-Reason", lockRow.Reason)
		}
	}

	if strings.Contains(r.Header.Get("Accept"), "text/markdown") {
		w.Header().Set("Content-Type", "text/markdown")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(page.Source))
		return
	}

	payload := map[string]any{
		"path":        page.Path,
		"file":        page.File,
		"title":       page.Title,
		"source":      page.Source,
		"summary":     page.Summary,
		"tags":        page.Tags,
		"version":     page.Version,
		"etag":        page.Version,
		"order":       page.Order,
		"frontmatter": page.Frontmatter,
	}
	if meta != nil {
		payload["last_actor"] = meta.LastActor
		payload["last_at"] = meta.LastAt
	}
	if approval != nil {
		payload["approval"] = map[string]any{
			"approved_by":   approval.ApprovedBy,
			"approved_at":   approval.ApprovedAt,
			"approved_etag": approval.ApprovedEtag,
			"stale":         approvalStale,
		}
	}
	if lockRow != nil {
		payload["lock"] = map[string]any{
			"locked_by": lockRow.LockedBy,
			"locked_at": lockRow.LockedAt,
			"reason":    lockRow.Reason,
		}
	}
	respondJSON(w, http.StatusOK, payload)
}

// handleUnifiedWrite is PUT /api/<path>. Body is raw bytes:
//   - text/markdown (or unspecified) → page tier (full-source write)
//   - application/json → data tier (singleton write with `{value}`
//     envelope or a frontmatter-only object)
//
// The dispatcher prefers existing-leaf tier match. Page-only when the
// path already exists as a page. Data-only when the path already
// exists in the store catalog. New writes pick page when the body
// looks like MDX (has a frontmatter block or explicit content-type
// text/markdown), otherwise data.
func (s *Server) handleUnifiedWrite(w http.ResponseWriter, r *http.Request) {
	path, _ := extractUnifiedPath(r)
	if path == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "path required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}

	target := s.dispatchTarget(path, body, r.Header.Get("Content-Type"))
	switch target {
	case "page":
		s.unifiedWritePage(w, r, path, string(body))
	case "data-item":
		s.unifiedWriteDataItem(w, r, path, body)
	case "data-singleton":
		s.unifiedWriteDataSingleton(w, r, path, body)
	default:
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "unable to route path: "+path)
	}
}

// dispatchTarget picks where a write should land. Decision tree:
//
//  1. Existing leaf wins — fetch as the same shape the catalog
//     reports (page > data-singleton > data-collection-item).
//  2. New write with content-type text/markdown or text/plain → page.
//  3. New write with content-type application/json AND the body
//     carries a top-level `value` key → data tier (the data envelope
//     shape; collection-item if the path has a slash, singleton if
//     flat).
//  4. Otherwise → page (the body is treated as raw MDX).
//
// The `{"value": ...}` signal is the disambiguator that lets a
// nested path like `kanban/task-1` write a data collection-item
// instead of a page. Without it the unified namespace can't tell
// `<path>/<id>` (data-item, JSON envelope) from `<folder>/<page>`
// (page tier, raw MDX).
func (s *Server) dispatchTarget(path string, body []byte, contentType string) string {
	// Existing leaf wins.
	if s.Pages != nil && s.Pages.GetPage(path) != nil {
		return "page"
	}
	if s.FileStore != nil {
		if _, ok := s.FileStore.CatalogGet(path); ok {
			return "data-singleton"
		}
		if _, _, ok := splitCollectionAddress(s.FileStore, path); ok {
			return "data-item"
		}
	}

	// Content-type hints.
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/markdown") || strings.Contains(ct, "text/plain") {
		return "page"
	}

	// JSON envelope shape `{"value": ...}` ⇒ data tier.
	if strings.Contains(ct, "application/json") && hasTopLevelValueKey(body) {
		if strings.Contains(path, "/") {
			return "data-item"
		}
		return "data-singleton"
	}

	// Bytes hint for missing/ambiguous content-type.
	trimmed := strings.TrimLeft(string(body), " \t\r\n")
	if strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "<") {
		return "page"
	}

	// Default: nested paths land in the page tier (handles arbitrary
	// nesting), flat keys in the data tier.
	if strings.Contains(path, "/") {
		return "page"
	}
	return "data-singleton"
}

// hasTopLevelValueKey reports whether `body` is a JSON object that
// includes a top-level `"value"` key. Used by dispatchTarget to
// distinguish a data-envelope write from a generic JSON page body.
func hasTopLevelValueKey(body []byte) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	_, ok := probe["value"]
	return ok
}

// unifiedWritePage runs the same post-write hooks the legacy
// /api/content/* handler did: PageMeta, Search, PageRefs, mention
// dispatch, SSE broadcast, lock check.
func (s *Server) unifiedWritePage(w http.ResponseWriter, r *http.Request, path, source string) {
	if e := s.enforcePageLock(r, store.NormalizePagePath(path)); e != nil {
		respondPageLocked(w, e)
		return
	}
	expected := ifMatch(r)
	if writeErr := s.Pages.WritePageIfMatch(path, source, expected); writeErr != nil {
		if errors.Is(writeErr, store.ErrPageStale) || errors.Is(writeErr, store.ErrPageNotFoundForMatch) {
			respondPageStale(w, s.Pages.GetPage(store.NormalizePagePath(path)), path)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", writeErr.Error())
		return
	}
	normalized := store.NormalizePagePath(path)
	actor := resolveActor(r)
	if s.PageMeta != nil {
		_ = s.PageMeta.Record(normalized, actor)
	}
	if s.Search != nil {
		if p := s.Pages.GetPage(normalized); p != nil {
			_ = s.Search.IndexPage(p.Path, p.Title, p.Source)
		}
	}
	if s.PageRefs != nil {
		if p := s.Pages.GetPage(normalized); p != nil {
			_ = s.PageRefs.Record(normalized, store.ExtractRefs(p.Source, normalized))
		}
	}
	subjectPath := "/" + normalized
	if normalized == "index" {
		subjectPath = "/"
	}
	s.emitInboxForMentions(source, actor, subjectPath, "", "You were mentioned on "+subjectPath)
	s.Broadcaster.Broadcast(SSEEvent{Type: "page-updated", Data: []byte(`{"path":"` + path + `"}`)})

	resp := map[string]any{"ok": true, "compiled": true}
	if page := s.Pages.GetPage(normalized); page != nil {
		resp["version"] = page.Version
		resp["etag"] = page.Version
		w.Header().Set("ETag", `"`+page.Version+`"`)
	}
	resp["last_actor"] = actor
	resp["warnings"] = store.CheckShape(normalized, s.frontmatterFor(normalized))
	respondJSON(w, http.StatusOK, resp)
}

// unifiedWriteDataSingleton is PUT against a flat-key singleton.
// Body is `{"value": ..., "_meta": {"version": "..."}}` — the same
// shape the legacy /api/data handler accepted.
func (s *Server) unifiedWriteDataSingleton(w http.ResponseWriter, r *http.Request, key string, body []byte) {
	var p struct {
		Meta  *store.Meta     `json:"_meta,omitempty"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", "body must be JSON: "+err.Error())
		return
	}
	if len(p.Value) == 0 {
		writeError(w, http.StatusBadRequest, "missing_value", `body must include {"value": ...}`)
		return
	}
	version := ""
	if p.Meta != nil {
		version = p.Meta.Version
	}
	hdr := strings.Trim(r.Header.Get("If-Match"), `"`)
	if hdr != "" {
		version = hdr
	}
	env, err := s.FileStore.Set(key, p.Value, version, actorFor(r))
	if err != nil {
		translateStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// unifiedWriteDataItem is PUT against a `<key>/<id>` collection-item
// path. Auto-creates the collection on first write — the caller hits
// /api/<key>/<id> with a data envelope and we route to UpsertItem
// even if the catalog hasn't seen `<key>` as a collection yet.
func (s *Server) unifiedWriteDataItem(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	idx := strings.LastIndex(path, "/")
	if idx <= 0 || idx == len(path)-1 {
		writeError(w, http.StatusBadRequest, "wrong_target", "expected <key>/<id> path")
		return
	}
	key := path[:idx]
	id := path[idx+1:]
	var p struct {
		Meta  *store.Meta     `json:"_meta,omitempty"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	if len(p.Value) == 0 {
		writeError(w, http.StatusBadRequest, "missing_value", `body must include {"value": ...}`)
		return
	}
	version := ""
	if p.Meta != nil {
		version = p.Meta.Version
	}
	hdr := strings.Trim(r.Header.Get("If-Match"), `"`)
	if hdr != "" {
		version = hdr
	}
	env, err := s.FileStore.UpsertItem(key, id, p.Value, version, actorFor(r))
	if err != nil {
		translateStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// handleUnifiedPatch is PATCH /api/<path>. Page tier: RFC-7396 merge
// into frontmatter + optional body replace. Data tier: RFC-7396 merge
// into the singleton's value, or `{"value": <patch>}` for the
// singleton's value blob.
func (s *Server) handleUnifiedPatch(w http.ResponseWriter, r *http.Request) {
	path, _ := extractUnifiedPath(r)
	if path == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "path required")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}

	// Page tier — frontmatter merge.
	if s.Pages != nil {
		if cur := s.Pages.GetPage(store.NormalizePagePath(path)); cur != nil {
			s.unifiedPatchPage(w, r, path, body, cur)
			return
		}
	}

	// Detect page-shape patches (`{"frontmatter_patch": ...}` or
	// `{"body": "..."}`) and treat them as 404 when the page tier
	// has no leaf. The legacy /api/content/* PATCH handler returned
	// 404 explicitly; the unified path keeps that contract so
	// callers don't see a misleading 400 missing_value.
	if isPagePatchBody(body) {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "page not found: "+path)
		return
	}

	// Data tier — value-patch.
	if s.FileStore != nil {
		var req struct {
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(body, &req); err != nil || len(req.Value) == 0 {
			writeError(w, http.StatusBadRequest, "missing_value", `body must be {"value": <patch>}`)
			return
		}
		if key, id, ok := splitCollectionAddress(s.FileStore, path); ok {
			env, err := s.FileStore.MergeItem(key, id, req.Value, actorFor(r))
			if err != nil {
				translateStoreError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, env)
			return
		}
		env, err := s.FileStore.Merge(path, req.Value, actorFor(r))
		if err != nil {
			translateStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, env)
		return
	}

	respondError(w, http.StatusNotFound, "NOT_FOUND", "no leaf at "+path)
}

// isPagePatchBody peeks at a JSON PATCH body to detect the
// `frontmatter_patch` / `body` shape. A patch with those keys is
// page-shaped; on a missing leaf the unified handler returns 404
// instead of "missing_value: 400" so the caller gets the right
// recovery hint.
func isPagePatchBody(body []byte) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	if _, ok := probe["frontmatter_patch"]; ok {
		return true
	}
	if _, ok := probe["body"]; ok {
		return true
	}
	return false
}

// unifiedPatchPage merges a JSON patch body into the page's
// frontmatter and/or replaces the body. Mirrors handlePatchPage's
// post-write hooks.
func (s *Server) unifiedPatchPage(w http.ResponseWriter, r *http.Request, path string, body []byte, current *store.PageInfo) {
	normalized := store.NormalizePagePath(path)
	if e := s.enforcePageLock(r, normalized); e != nil {
		respondPageLocked(w, e)
		return
	}
	var req struct {
		FrontmatterPatch map[string]any `json:"frontmatter_patch"`
		Body             *string        `json:"body"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "patch body must be JSON: "+err.Error())
		return
	}
	if req.FrontmatterPatch == nil && req.Body == nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "patch must set frontmatter_patch and/or body")
		return
	}
	merged := map[string]any{}
	for k, v := range current.Frontmatter {
		merged[k] = v
	}
	for k, v := range req.FrontmatterPatch {
		if v == nil {
			delete(merged, k)
		} else {
			merged[k] = v
		}
	}
	pageBody := current.Source
	if req.Body != nil {
		pageBody = *req.Body
	}
	newSource, err := store.AssemblePageSource(merged, pageBody)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "assemble source: "+err.Error())
		return
	}
	expected := ifMatch(r)
	if writeErr := s.Pages.WritePageIfMatch(path, newSource, expected); writeErr != nil {
		if errors.Is(writeErr, store.ErrPageStale) || errors.Is(writeErr, store.ErrPageNotFoundForMatch) {
			respondPageStale(w, s.Pages.GetPage(normalized), path)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", writeErr.Error())
		return
	}
	actor := resolveActor(r)
	if s.PageMeta != nil {
		_ = s.PageMeta.Record(normalized, actor)
	}
	if s.Search != nil {
		if p := s.Pages.GetPage(normalized); p != nil {
			_ = s.Search.IndexPage(p.Path, p.Title, p.Source)
		}
	}
	if s.PageRefs != nil {
		if p := s.Pages.GetPage(normalized); p != nil {
			_ = s.PageRefs.Record(normalized, store.ExtractRefs(p.Source, normalized))
		}
	}
	subjectPath := "/" + normalized
	if normalized == "index" {
		subjectPath = "/"
	}
	s.emitInboxForMentions(newSource, actor, subjectPath, "", "You were mentioned on "+subjectPath)
	s.Broadcaster.Broadcast(SSEEvent{Type: "page-updated", Data: []byte(`{"path":"` + path + `"}`)})

	resp := map[string]any{"ok": true, "compiled": true}
	if page := s.Pages.GetPage(normalized); page != nil {
		resp["version"] = page.Version
		resp["etag"] = page.Version
		w.Header().Set("ETag", `"`+page.Version+`"`)
	}
	resp["last_actor"] = actor
	resp["warnings"] = store.CheckShape(normalized, s.frontmatterFor(normalized))
	respondJSON(w, http.StatusOK, resp)
}

// handleUnifiedDelete is DELETE /api/<path>. Page tier: drop the page
// + meta + approval + refs + FTS row. Data tier: drop the leaf at the
// matching shape.
func (s *Server) handleUnifiedDelete(w http.ResponseWriter, r *http.Request) {
	path, _ := extractUnifiedPath(r)
	if path == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "path required")
		return
	}
	if path == "index" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "cannot delete index page")
		return
	}

	// Page tier first.
	if s.Pages != nil {
		if s.Pages.GetPage(store.NormalizePagePath(path)) != nil {
			s.unifiedDeletePage(w, r, path)
			return
		}
	}

	// Data tier — singleton, item, collection, stream.
	if s.FileStore != nil {
		actor := actorFor(r)
		if key, id, ok := splitCollectionAddress(s.FileStore, path); ok {
			version := r.URL.Query().Get("version")
			if version == "" {
				version = "*"
			}
			if err := s.FileStore.DeleteItem(key, id, version, actor); err != nil {
				translateStoreError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if cat, ok := s.FileStore.CatalogGet(path); ok {
			version := r.URL.Query().Get("version")
			if version == "" {
				version = "*"
			}
			var err error
			switch cat.Shape {
			case store.ShapeSingleton:
				err = s.FileStore.DeleteSingleton(path, version, actor)
			case store.ShapeStream:
				err = s.FileStore.DeleteStream(path, actor)
			case store.ShapeCollection:
				if r.URL.Query().Get("confirm") != "true" {
					writeError(w, http.StatusBadRequest, "confirmation_required",
						"deleting a whole collection requires ?confirm=true")
					return
				}
				err = s.FileStore.DeleteCollection(path, actor)
			}
			if err != nil {
				translateStoreError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	// Idempotent: nothing to delete is success.
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unifiedDeletePage(w http.ResponseWriter, r *http.Request, path string) {
	normalized := store.NormalizePagePath(path)
	if e := s.enforcePageLock(r, normalized); e != nil {
		respondPageLocked(w, e)
		return
	}
	expected := ifMatch(r)
	if delErr := s.Pages.DeletePageIfMatch(path, expected); delErr != nil {
		if errors.Is(delErr, store.ErrPageStale) || errors.Is(delErr, store.ErrPageNotFoundForMatch) {
			respondPageStale(w, s.Pages.GetPage(normalized), path)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", delErr.Error())
		return
	}
	if s.PageMeta != nil {
		_ = s.PageMeta.Delete(normalized)
	}
	if s.PageApproval != nil {
		_ = s.PageApproval.Delete(normalized)
	}
	if s.PageRefs != nil {
		_ = s.PageRefs.Delete(normalized)
	}
	if s.Search != nil {
		_ = s.Search.DeletePage("/" + normalized)
	}
	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-updated",
		Data: []byte(`{"path":"` + path + `","deleted":true}`),
	})
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleUnifiedAppend is POST /api/<path>:append. Streams only.
func (s *Server) handleUnifiedAppend(w http.ResponseWriter, r *http.Request) {
	path, isAppend := extractUnifiedPath(r)
	if !isAppend {
		respondError(w, http.StatusBadRequest, "WRONG_VERB", "POST requires :append suffix on /api/<path>")
		return
	}
	if path == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "path required")
		return
	}
	if s.FileStore == nil {
		respondError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "store not configured")
		return
	}

	var body struct {
		Value json.RawMessage   `json:"value"`
		Items []json.RawMessage `json:"items"`
	}
	if err := readJSONBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", err.Error())
		return
	}
	actor := actorFor(r)
	if body.Items != nil {
		lines, err := s.FileStore.AppendBatch(path, body.Items, actor)
		if err != nil {
			translateStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"appended": len(lines), "lines": lines})
		return
	}
	if len(body.Value) == 0 {
		writeError(w, http.StatusBadRequest, "missing_value", `pass {"value": ...} or {"items": [...]}`)
		return
	}
	line, err := s.FileStore.Append(path, body.Value, actor)
	if err != nil {
		translateStoreError(w, err)
		return
	}
	s.dispatchInboxForValueWrite(path, body.Value, actor, "")
	writeJSON(w, http.StatusOK, line)
}

// handleUnifiedHistory serves GET /api/<path>/history per spec §5.
// Routes to the per-doc audit log via the data store. Pages share
// the same history index (writes go through both layers).
func (s *Server) handleUnifiedHistory(w http.ResponseWriter, r *http.Request, key string) {
	if s.FileStore == nil {
		respondError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "history store not configured")
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := parseLimit(raw); err == nil {
			limit = n
		}
	}
	// `<key>/<id>` — collection-item history.
	if k, id, ok := splitCollectionAddress(s.FileStore, key); ok {
		entries, err := s.FileStore.ReadHistory(k, id, limit)
		if err != nil {
			translateStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"key": k, "id": id, "entries": entries, "count": len(entries)})
		return
	}
	entries, err := s.FileStore.ReadHistory(key, "", limit)
	if err != nil {
		translateStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "entries": entries, "count": len(entries)})
}

// parseLimit accepts a positive integer string from a `?limit=` query.
func parseLimit(s string) (int, error) {
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("limit must be digits")
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return 0, errors.New("limit must be > 0")
	}
	return n, nil
}

// splitCollectionAddress is the same logic the MCP layer uses
// (handlers.go::splitCollectionPath) — exposed here so REST can route
// `<key>/<id>` paths to UpsertItem / MergeItem / DeleteItem.
func splitCollectionAddress(s *store.Store, path string) (key, id string, ok bool) {
	if s == nil {
		return "", "", false
	}
	idx := strings.LastIndex(path, "/")
	if idx <= 0 || idx == len(path)-1 {
		return "", "", false
	}
	k := path[:idx]
	i := path[idx+1:]
	cat, exists := s.CatalogGet(k)
	if !exists || cat.Shape != store.ShapeCollection {
		return "", "", false
	}
	return k, i, true
}

// frontmatterFor returns the most recently scanned page's frontmatter
// for the given normalized path, or nil. Used to feed CheckShape from
// inside a write handler after the rescan.
func (s *Server) frontmatterFor(normalized string) map[string]any {
	if s.Pages == nil {
		return nil
	}
	p := s.Pages.GetPage(normalized)
	if p == nil {
		return nil
	}
	return p.Frontmatter
}

// suppressUnusedAuth keeps the auth import alive even when no
// auth-specific symbol is referenced — the package is consumed via
// resolveActor + actorFor in the same package.
var _ = filepath.Separator
var _ = auth.UserFromContext
