package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/mdx"
)

// resolveActor picks a human-readable identity for a write. Prefers the
// authenticated user's username when auth middleware populated it; falls back
// to the X-Agent-Source header for MCP/script callers that don't carry a
// user; last resort is "anonymous".
func resolveActor(r *http.Request) string {
	if u := auth.UserFromContext(r.Context()); u != nil {
		return u.Username
	}
	if s := r.Header.Get("X-Agent-Source"); s != "" {
		return s
	}
	return "anonymous"
}

// respondPageStale emits a 412 with the current page info so the caller can
// re-base and retry. page may be nil when the path doesn't exist.
func respondPageStale(w http.ResponseWriter, page *mdx.PageInfo, path string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPreconditionFailed)
	payload := map[string]any{
		"code":  "STALE_WRITE",
		"error": "If-Match did not match current page etag",
		"path":  path,
	}
	if page != nil {
		payload["current"] = page
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleListPages(w http.ResponseWriter, r *http.Request) {
	pages := s.Pages.ListPages()

	// ?prefix=features/components — subtree filter. Kept matched against
	// the URL path (what callers see) rather than the disk path so a client
	// that already knows `/features/components` doesn't need to strip the
	// leading slash.
	if prefix := r.URL.Query().Get("prefix"); prefix != "" {
		norm := "/" + strings.TrimPrefix(prefix, "/")
		filtered := make([]mdx.PageInfo, 0, len(pages))
		for _, p := range pages {
			if strings.HasPrefix(p.Path, norm) {
				filtered = append(filtered, p)
			}
		}
		pages = filtered
	}

	// ?fields=path,title drops source bodies when the caller only needs a
	// lightweight manifest. Saves bandwidth on large projects (see
	// DOGFOOD_NOTES — 109 KB dropped to a few KB).
	if fields := r.URL.Query().Get("fields"); fields != "" {
		allowed := map[string]bool{}
		for _, f := range strings.Split(fields, ",") {
			allowed[strings.TrimSpace(f)] = true
		}
		out := make([]map[string]any, 0, len(pages))
		for _, p := range pages {
			row := map[string]any{}
			if allowed["path"] {
				row["path"] = p.Path
			}
			if allowed["file"] {
				row["file"] = p.File
			}
			if allowed["title"] {
				row["title"] = p.Title
			}
			if allowed["source"] {
				row["source"] = p.Source
			}
			if allowed["etag"] {
				row["etag"] = p.Etag
			}
			if allowed["order"] {
				row["order"] = p.Order
			}
			out = append(out, row)
		}
		respondJSON(w, http.StatusOK, out)
		return
	}

	respondJSON(w, http.StatusOK, pages)
}

func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimPrefix(r.URL.Path, "/api/content/")
	if pagePath == "" {
		pagePath = "index"
	}

	page := s.Pages.GetPage(pagePath)
	if page == nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "page not found: "+pagePath)
		return
	}

	// Content-addressed etag — round-trip as If-Match on the caller's next write.
	if page.Etag != "" {
		w.Header().Set("ETag", `"`+page.Etag+`"`)
	}

	// Best-effort last-edited-by. Emit as headers so `Accept: text/markdown`
	// callers still get the info without parsing JSON.
	var meta *mdx.PageMeta
	if s.PageMeta != nil {
		meta, _ = s.PageMeta.Get(pagePath)
		if meta != nil {
			w.Header().Set("X-Last-Actor", meta.LastActor)
			w.Header().Set("X-Last-At", meta.LastAt)
		}
	}
	// Approval, if any. Stale = the approved etag no longer matches the
	// page's current etag — show "approved at vX, edited since" in the UI.
	var approval *mdx.PageApproval
	var approvalStale bool
	if s.PageApproval != nil {
		approval, _ = s.PageApproval.Get(pagePath)
		if approval != nil {
			approvalStale = approval.ApprovedEtag != page.Etag
			w.Header().Set("X-Approved-By", approval.ApprovedBy)
			w.Header().Set("X-Approved-At", approval.ApprovedAt)
			w.Header().Set("X-Approved-Etag", approval.ApprovedEtag)
			if approvalStale {
				w.Header().Set("X-Approval-Stale", "true")
			}
		}
	}

	// Check Accept header to determine response format
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/markdown") {
		w.Header().Set("Content-Type", "text/markdown")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(page.Source))
		return
	}

	// Default: return page metadata + source + last-edited meta.
	payload := map[string]any{
		"path":   page.Path,
		"file":   page.File,
		"title":  page.Title,
		"source": page.Source,
		"etag":   page.Etag,
		"order":  page.Order,
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
	respondJSON(w, http.StatusOK, payload)
}

func (s *Server) handleWritePage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimPrefix(r.URL.Path, "/api/content/")
	if pagePath == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "page path required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "could not read body")
		return
	}

	expected := ifMatch(r)
	writeErr := s.Pages.WritePageIfMatch(pagePath, string(body), expected)
	if writeErr != nil {
		if errors.Is(writeErr, mdx.ErrStale) || errors.Is(writeErr, mdx.ErrNotFoundForMatch) {
			respondPageStale(w, s.Pages.GetPage(strings.TrimSuffix(pagePath, ".md")), pagePath)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", writeErr.Error())
		return
	}

	// Record last-edited-by before broadcasting so readers hitting the SSE
	// refetch see the new meta right away. Best-effort; a meta failure
	// doesn't roll back the write.
	normalizedPath := strings.TrimSuffix(pagePath, ".md")
	actor := resolveActor(r)
	if s.PageMeta != nil {
		_ = s.PageMeta.Record(normalizedPath, actor)
	}

	// Keep the FTS index in lockstep with the write. Best-effort; if the
	// index drifts, a server restart rebuilds from disk.
	if s.Search != nil {
		if p := s.Pages.GetPage(normalizedPath); p != nil {
			_ = s.Search.IndexPage(p.Path, p.Title, p.Source)
		}
	}

	// Refresh the static dependency graph so the view broker knows what
	// this page references. Best-effort; view broker degrades silently
	// if refs drift.
	if s.PageRefs != nil {
		if p := s.Pages.GetPage(normalizedPath); p != nil {
			_ = s.PageRefs.Record(normalizedPath, mdx.ExtractRefs(p.Source))
		}
	}

	// Mention detection on the new MDX source. Every @username in the
	// post-write body that maps to an active user gets an inbox item.
	subjectPath := "/" + normalizedPath
	if normalizedPath == "index" {
		subjectPath = "/"
	}
	title := "You were mentioned on " + subjectPath
	s.emitInboxForMentions(string(body), actor, subjectPath, "", title)

	// Broadcast page update via SSE
	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-updated",
		Data: []byte(`{"path":"` + pagePath + `"}`),
	})

	// Echo the new etag so clients don't need a follow-up GET.
	page := s.Pages.GetPage(normalizedPath)
	resp := map[string]any{"ok": true, "compiled": true}
	if page != nil {
		resp["etag"] = page.Etag
		w.Header().Set("ETag", `"`+page.Etag+`"`)
	}
	resp["last_actor"] = actor
	respondJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeletePage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimPrefix(r.URL.Path, "/api/content/")

	if pagePath == "index" || pagePath == "index.md" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "cannot delete index page")
		return
	}

	expected := ifMatch(r)
	delErr := s.Pages.DeletePageIfMatch(pagePath, expected)
	if delErr != nil {
		if errors.Is(delErr, mdx.ErrStale) || errors.Is(delErr, mdx.ErrNotFoundForMatch) {
			respondPageStale(w, s.Pages.GetPage(strings.TrimSuffix(pagePath, ".md")), pagePath)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", delErr.Error())
		return
	}

	// Drop the meta + approval rows too — a recreated page should start
	// with a fresh attribution rather than inherit the deleter, and
	// absolutely shouldn't carry a stale approval across identity-reuse.
	normalizedPath := strings.TrimSuffix(pagePath, ".md")
	if s.PageMeta != nil {
		_ = s.PageMeta.Delete(normalizedPath)
	}
	if s.PageApproval != nil {
		_ = s.PageApproval.Delete(normalizedPath)
	}
	if s.PageRefs != nil {
		_ = s.PageRefs.Delete(normalizedPath)
	}
	if s.Search != nil {
		// FTS path is stored with a leading slash (matches PageInfo.Path).
		_ = s.Search.DeletePage("/" + normalizedPath)
	}

	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-updated",
		Data: []byte(`{"path":"` + pagePath + `","deleted":true}`),
	})

	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleMovePage renames/moves a page file. Body: {"from": "...", "to": "..."}.
// Returns 404 if the source doesn't exist, 409 if the destination does, 400
// for invalid paths (empty, index, or containing "..").
func (s *Server) handleMovePage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "expected JSON body {from, to}")
		return
	}

	from := normalizePagePath(body.From)
	to := normalizePagePath(body.To)

	if from == "" || to == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "both from and to are required")
		return
	}
	if from == "index" || to == "index" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "cannot move to or from the index page")
		return
	}
	if strings.Contains(from, "..") || strings.Contains(to, "..") {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "path must not contain '..'")
		return
	}
	if from == to {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "from and to must differ")
		return
	}

	if err := s.Pages.MovePage(from, to); err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			respondError(w, http.StatusNotFound, "NOT_FOUND", "source page does not exist: "+from)
		case errors.Is(err, os.ErrExist):
			respondError(w, http.StatusConflict, "CONFLICT", "destination already exists: "+to)
		default:
			respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
		return
	}

	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-updated",
		Data: []byte(`{"from":"` + from + `","to":"` + to + `","moved":true}`),
	})

	respondJSON(w, http.StatusOK, map[string]any{"ok": true, "from": from, "to": to})
}

// normalizePagePath trims a leading slash, trailing ".md", and surrounding
// whitespace so inputs like "/features/old", "features/old.md", or " features/old "
// all resolve to the canonical "features/old".
func normalizePagePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, ".md")
	return p
}
