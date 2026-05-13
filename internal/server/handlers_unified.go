package server

// Reads on the unified /api/<path> namespace (spec §5).
//
// What used to be the catch-all CRUD surface (PUT/PATCH/DELETE/POST
// /api/<path>) was retired in the git-substrate pivot: writes go
// through `git push` for agents that can shell out, or
// `agentboard_propose` over MCP for runtimes that can't. This file
// is now exclusively the read side:
//
//   - GET /api/<path>             → page envelope (or 404)
//   - GET /api/<path>/history     → per-doc audit log via FileStore
//                                   (kept while the legacy FileStore
//                                    history is the only authority)
//
// Reserved /api/* prefixes (admin, auth, view, files, components, etc.)
// are registered before this catch-all wildcard so they win the chi
// dispatcher; everything else is content read.
//
// The data-tier branches (singleton / collection / stream / item)
// stay alive in the read path until streams are migrated into the
// git working tree.

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/christophermarx/agentboard/internal/locks"
	"github.com/christophermarx/agentboard/internal/store"
	"github.com/go-chi/chi/v5"
)

// extractUnifiedPath pulls the leaf path from a chi catch-all route.
// Applies the SKILL.md → folder collapse via store.NormalizePagePath
// so `/api/skills/<slug>/SKILL.md` resolves to the skill's folder
// index page (spec §1 path layout). The trailing `:append` token used
// to mean a stream-append POST — that route is gone now but the
// helper still strips the suffix for any caller that grew up against
// the old shape.
func extractUnifiedPath(r *http.Request) (path string, isAppend bool) {
	raw := chi.URLParam(r, "*")
	if strings.HasSuffix(raw, ":append") {
		return store.NormalizePagePath(strings.TrimSuffix(raw, ":append")), true
	}
	return store.NormalizePagePath(raw), false
}

// handleUnifiedRead is GET /api/<path>. Returns the page envelope for
// a page leaf, the singleton/collection/stream payload for a data
// leaf, 404 for anything else.
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

	// Page tier first — the worktree mirror is the live source.
	if s.Pages != nil {
		if page := s.Pages.GetPage(path); page != nil {
			s.respondUnifiedPage(w, r, page)
			return
		}
	}

	// Streams (and any legacy singletons / collections still hanging
	// around from pre-pivot installs) — kept alive until streams
	// migrate into the git tree.
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

// respondUnifiedPage emits the page-tier read envelope. ETag, meta
// headers, approval / lock indicators, and the optional
// `Accept: text/markdown` raw-source branch.
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

// handleUnifiedHistory serves GET /api/<path>/history. The audit log
// stays in the FileStore for now; once the git working tree owns
// per-leaf history (`git log -- <path>`), this whole handler can
// disappear.
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

// splitCollectionAddress is shared with the MCP layer
// (handlers.go::splitCollectionPath). Returns (key, id, true) when
// `path` looks like `<collection>/<item>` and `<collection>` exists in
// the FileStore catalog.
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
