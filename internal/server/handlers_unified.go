package server

// Cut 7: REST namespace unification. Spec §5 says one namespace —
// `/api/<path>` GET/PUT/PATCH/DELETE/POST :append — covers the entire
// content tier.
//
// Cut 11 (CORE_GUIDELINES §14): write dispatch collapses. Every write
// lands as a `.md` page leaf in the page tree. JSON `{"value": …}`
// bodies are translated to YAML frontmatter at the boundary. The old
// "data tier" branch (singleton .md files in `<project>/data/`) is
// retired — pages are the only authoring shape. Streams stay because
// append-atomically is a different storage problem; binaries stay as
// uploaded blobs. Reads still resolve existing FileStore singletons
// during the migration window.
//
// Reserved /api/* prefixes (admin, auth, view, files, components, etc.)
// are registered before this catch-all wildcard so they win the chi
// dispatcher; everything else is content.

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

// handleUnifiedWrite is PUT /api/<path>. Every write lands as a `.md`
// page leaf. Two body shapes are accepted:
//
//   - text/markdown / text/plain / unspecified → raw MDX source. The
//     body is written as-is (frontmatter block + body) into the
//     page tree.
//   - application/json `{"value": <X>}` (with optional `_meta.version`)
//     → translated to a frontmatter-only page with `value: <X>`. This
//     keeps the legacy data-envelope wire shape working while the
//     leaf itself lives as a page (per CORE_GUIDELINES §14).
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

	// JSON `{"value": …}` envelope → translate to MDX frontmatter so
	// the leaf lands as a page. Honors `_meta.version` as If-Match,
	// matching the legacy data-tier behavior.
	if isValueEnvelopeBody(body, r.Header.Get("Content-Type")) {
		src, version, terr := translateValueEnvelopeToPageSource(body)
		if terr != nil {
			writeError(w, http.StatusBadRequest, "bad_body", terr.Error())
			return
		}
		if version != "" && r.Header.Get("If-Match") == "" {
			r.Header.Set("If-Match", `"`+version+`"`)
		}
		s.unifiedWritePage(w, r, path, src)
		return
	}

	s.unifiedWritePage(w, r, path, string(body))
}

// isValueEnvelopeBody reports whether a body should be treated as a
// `{"value": …}` data envelope rather than raw MDX. Triggers on JSON
// content-type with a top-level `value` key, OR on an unspecified
// content-type when the body parses as such an object (so REST callers
// who forget the header still route correctly).
func isValueEnvelopeBody(body []byte, contentType string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/markdown") || strings.Contains(ct, "text/plain") {
		return false
	}
	if !strings.Contains(ct, "application/json") {
		// No content-type hint either way — peek at the bytes. MDX
		// pages typically start with `---` (frontmatter), `#`, or `<`;
		// JSON starts with `{`.
		trimmed := strings.TrimLeft(string(body), " \t\r\n")
		if !strings.HasPrefix(trimmed, "{") {
			return false
		}
	}
	return hasTopLevelValueKey(body)
}

// translateValueEnvelopeToPageSource converts `{"value": <X>, "_meta": {…}}`
// JSON into an MDX source with `value: <X>` in the frontmatter and an
// empty body. Returns the assembled source plus the optional version
// (so the caller can echo it as If-Match).
func translateValueEnvelopeToPageSource(body []byte) (string, string, error) {
	var p struct {
		Meta  *store.Meta     `json:"_meta,omitempty"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return "", "", errors.New("body must be JSON: " + err.Error())
	}
	if len(p.Value) == 0 {
		return "", "", errors.New(`body must include {"value": …}`)
	}
	var decoded any
	if err := json.Unmarshal(p.Value, &decoded); err != nil {
		return "", "", errors.New("could not decode value: " + err.Error())
	}
	src, err := store.AssemblePageSource(map[string]any{"value": decoded}, "")
	if err != nil {
		return "", "", err
	}
	version := ""
	if p.Meta != nil {
		version = p.Meta.Version
	}
	return src, version, nil
}

// hasTopLevelValueKey reports whether `body` is a JSON object that
// includes a top-level `"value"` key.
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

// handleUnifiedPatch is PATCH /api/<path>. Three accepted body shapes:
//
//   - `{"frontmatter_patch": …, "body": …}` → page-shaped patch.
//   - `{"value": <patch>}` → translated to `frontmatter_patch: {value: <patch>}`
//     so the legacy data-envelope wire shape continues to work
//     against pages (per CORE_GUIDELINES §14).
//   - All-frontmatter shapes (no `frontmatter_patch` key, no `body`,
//     no `value`) — rejected.
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

	// `{"value": <patch>}` → translate to a page frontmatter patch so
	// the legacy data-envelope wire shape merges into the page's
	// `value` frontmatter field instead of routing to a separate tier.
	if translated, ok := translateValuePatchToPageFrontmatter(body); ok {
		body = translated
	}

	if s.Pages != nil {
		if cur := s.Pages.GetPage(store.NormalizePagePath(path)); cur != nil {
			s.unifiedPatchPage(w, r, path, body, cur)
			return
		}
	}

	respondError(w, http.StatusNotFound, "NOT_FOUND", "page not found: "+path)
}

// translateValuePatchToPageFrontmatter converts a `{"value": <patch>}`
// PATCH body into `{"frontmatter_patch": {"value": <patch>}}` so the
// legacy data-envelope wire shape lands as a frontmatter merge on a
// page leaf. Returns ok=false when the body is already page-shaped or
// not a value patch.
func translateValuePatchToPageFrontmatter(body []byte) ([]byte, bool) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, false
	}
	if _, ok := probe["frontmatter_patch"]; ok {
		return nil, false
	}
	if _, ok := probe["body"]; ok {
		return nil, false
	}
	raw, ok := probe["value"]
	if !ok {
		return nil, false
	}
	out, err := json.Marshal(map[string]any{
		"frontmatter_patch": map[string]json.RawMessage{"value": raw},
	})
	if err != nil {
		return nil, false
	}
	return out, true
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
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", `patch must set "frontmatter_patch" and/or "body" (or {"value": <patch>} which is translated to a frontmatter merge)`)
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

// handleUnifiedDelete is DELETE /api/<path>. Drops the page leaf if
// present (along with meta + approval + refs + FTS row), or the
// stream leaf when the FileStore catalogs it as a stream. Existing
// singleton / collection FileStore leaves from before the §14
// dispatcher collapse are still deletable here so operators can clean
// them up; new writes never create those shapes anymore.
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

	// Stream + legacy-singleton cleanup path.
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
