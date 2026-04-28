package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Bulk delete endpoints. Agents cleaning up after a mis-scoped write —
// or an operator pruning a stale key namespace — don't want to make 50
// round-trips. Pass either `paths`/`keys` (explicit list) or `prefix`
// (subtree), optionally `dry_run: true` to preview without deleting.
//
// Response shape is identical in dry-run mode — callers can pipe the
// exact same request without the flag to commit.

type bulkRequest struct {
	Paths  []string `json:"paths,omitempty"`
	Keys   []string `json:"keys,omitempty"`
	Prefix string   `json:"prefix,omitempty"`
	DryRun bool     `json:"dry_run,omitempty"`
}

type bulkResponse struct {
	Deleted []string `json:"deleted"`
	Skipped []string `json:"skipped,omitempty"` // e.g. protected index page
	DryRun  bool     `json:"dry_run,omitempty"`
}

// handleBulkDeleteContent deletes one or more pages in a single call.
//
//	POST /api/content/bulk-delete
//	{ "paths": ["/foo", "/bar"] }
//	{ "prefix": "/scratch" }
//	{ "prefix": "/scratch", "dry_run": true }
func (s *Server) handleBulkDeleteContent(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "expected JSON { paths?, prefix?, dry_run? }")
		return
	}

	// Collect targets. Explicit `paths` wins if provided; otherwise walk
	// the manifest with the prefix filter.
	var targets []string
	if len(req.Paths) > 0 {
		targets = append(targets, req.Paths...)
	} else if req.Prefix != "" {
		norm := "/" + strings.TrimPrefix(req.Prefix, "/")
		for _, p := range s.Pages.ListPages() {
			if strings.HasPrefix(p.Path, norm) {
				targets = append(targets, p.Path)
			}
		}
	} else {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "provide `paths` or `prefix`")
		return
	}

	// All-or-nothing lock check for non-admins: if ANY target in this
	// batch is locked, reject the whole request. Prevents a partial
	// delete that leaves the batch in a mixed state.
	if s.Locks != nil {
		for _, p := range targets {
			normalized := strings.TrimPrefix(strings.TrimSuffix(p, ".md"), "/")
			if e := s.enforcePageLock(r, normalized); e != nil {
				respondPageLocked(w, e)
				return
			}
		}
	}

	out := bulkResponse{Deleted: []string{}, Skipped: []string{}, DryRun: req.DryRun}
	actor := resolveActor(r)
	for _, p := range targets {
		pagePath := strings.TrimPrefix(p, "/")
		if pagePath == "" || pagePath == "index" || pagePath == "index.md" {
			out.Skipped = append(out.Skipped, p)
			continue
		}
		if req.DryRun {
			if s.Pages.GetPage(strings.TrimSuffix(pagePath, ".md")) != nil {
				out.Deleted = append(out.Deleted, p)
			}
			continue
		}
		if err := s.Pages.DeletePage(pagePath); err != nil {
			// Swallow "not found" — keep going. Other errors still
			// count as "attempted" but we list them in Skipped for
			// visibility.
			out.Skipped = append(out.Skipped, p)
			continue
		}
		out.Deleted = append(out.Deleted, p)
		normalized := strings.TrimSuffix(pagePath, ".md")
		if s.PageMeta != nil {
			_ = s.PageMeta.Delete(normalized)
		}
		if s.Search != nil {
			_ = s.Search.DeletePage("/" + normalized)
		}
		s.Broadcaster.Broadcast(SSEEvent{
			Type: "page-updated",
			Data: []byte(`{"path":"` + pagePath + `","deleted":true,"actor":"` + actor + `"}`),
		})
	}

	respondJSON(w, http.StatusOK, out)
}

// handleBulkDeleteFiles — same shape for uploaded files.
func (s *Server) handleBulkDeleteFiles(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "files manager not configured")
		return
	}
	var req bulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "expected JSON { paths?, prefix?, dry_run? }")
		return
	}

	var targets []string
	if len(req.Paths) > 0 {
		targets = append(targets, req.Paths...)
	} else if req.Prefix != "" {
		norm := strings.TrimPrefix(req.Prefix, "/")
		list, err := s.Files.List()
		if err != nil {
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		for _, f := range list {
			if strings.HasPrefix(f.Name, norm) {
				targets = append(targets, f.Name)
			}
		}
	} else {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "provide `paths` or `prefix`")
		return
	}

	out := bulkResponse{Deleted: []string{}, Skipped: []string{}, DryRun: req.DryRun}
	for _, name := range targets {
		name = strings.TrimPrefix(name, "/")
		if req.DryRun {
			if _, err := s.Files.Stat(name); err == nil {
				out.Deleted = append(out.Deleted, name)
			}
			continue
		}
		if err := s.Files.Delete(name); err != nil {
			out.Skipped = append(out.Skipped, name)
			continue
		}
		out.Deleted = append(out.Deleted, name)
		s.Broadcaster.Broadcast(SSEEvent{
			Type: "file-updated",
			Data: []byte(`{"name":"` + name + `","deleted":true}`),
		})
	}

	respondJSON(w, http.StatusOK, out)
}

// handleBulkDeleteData — DEPRECATED stub. The legacy KV layer is gone
// in the rewrite; the route entry itself gets removed in Cut 1.D.
// Until then, the handler returns 410 Gone with a clear forward
// pointer to the v2 surface.
func (s *Server) handleBulkDeleteData(w http.ResponseWriter, r *http.Request) {
	_ = r
	respondError(w, http.StatusGone, "REMOVED",
		"the legacy /api/data/* surface was removed in the rewrite — use /api/data/* (or /api/* once Cut 3 lands)")
}
