package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Helpers for arg extraction. The arg map is JSON-RawMessage-valued so
// every tool can decode just the keys it cares about and ignore the
// rest.

func getString(args map[string]json.RawMessage, key string) string {
	raw, ok := args[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func getStringList(args map[string]json.RawMessage, key string) []string {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// mcpJSON wraps a plain value in the MCP tool-result envelope. MCP
// tools return `{content: [{type: "text", text: <json>}]}`; the LLM
// receives the text representation. We always emit JSON so structured
// results round-trip cleanly.
func mcpJSON(v any) any {
	b, _ := json.MarshalIndent(v, "", "  ")
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(b)},
		},
	}
}

// ---------- agentboard_workspaces ----------

func (s *Server) toolWorkspaces(r *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	if s.GitStore == nil {
		return mcpJSON(map[string]any{"workspaces": []any{}}), nil
	}
	list, err := s.GitStore.List(r.Context())
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: "list workspaces: " + err.Error()}
	}
	base := s.PublicBaseURL
	if base == "" && r != nil {
		// Best-effort default: assume the same scheme + host the MCP
		// call landed on. Cowork and other allowlist-aware clients
		// already know the host, so this is just convenience.
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		base = fmt.Sprintf("%s://%s", scheme, r.Host)
	}
	out := make([]map[string]any, 0, len(list))
	for _, ws := range list {
		entry := map[string]any{
			"id":             ws.ID,
			"default_branch": ws.DefaultBranch,
			"policy":         ws.Policy,
		}
		if base != "" {
			entry["clone_url"] = fmt.Sprintf("%s/git/%s.git", base, ws.ID)
			entry["bootstrap_hint"] = fmt.Sprintf(
				"git clone https://_:$AGENTBOARD_TOKEN@%s/git/%s.git && cd %s && cat README.md",
				strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://"),
				ws.ID, ws.ID,
			)
		}
		out = append(out, entry)
	}
	return mcpJSON(map[string]any{"workspaces": out}), nil
}

// ---------- agentboard_pull ----------

func (s *Server) toolPull(r *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	ws := getString(args, "workspace")
	if ws == "" {
		return nil, &RPCError{Code: -32602, Message: "workspace required"}
	}
	if s.GitStore == nil {
		return nil, &RPCError{Code: -32000, Message: "git substrate not configured"}
	}
	w, err := s.GitStore.Get(r.Context(), ws)
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: "lookup: " + err.Error()}
	}
	if w == nil {
		return nil, &RPCError{Code: -32602, Message: "no such workspace: " + ws}
	}
	wtPath, err := s.GitStore.EnsureWorktree(r.Context(), ws)
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: "ensure worktree: " + err.Error()}
	}

	type fileOut struct {
		Path        string         `json:"path"`
		Frontmatter map[string]any `json:"frontmatter,omitempty"`
		Body        string         `json:"body"`
	}
	var files []fileOut
	const maxFiles = 500
	const maxBytes = 5 * 1024 * 1024
	totalBytes := 0
	err = filepath.Walk(wtPath, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxFiles || totalBytes >= maxBytes {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(wtPath, p)
		rel = filepath.ToSlash(rel)
		// Only return .md leaves with parsed frontmatter; everything
		// else (binaries, jsx components, ndjson streams) is returned
		// with body-as-string when small, omitted otherwise.
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil // skip unreadable files; non-fatal
		}
		totalBytes += len(raw)
		fo := fileOut{Path: rel}
		if strings.HasSuffix(rel, ".md") {
			fm, body := splitFrontmatter(raw)
			fo.Frontmatter = fm
			fo.Body = body
		} else {
			fo.Body = string(raw)
		}
		files = append(files, fo)
		return nil
	})
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: "walk worktree: " + err.Error()}
	}
	return mcpJSON(map[string]any{
		"workspace": ws,
		"ref":       w.DefaultBranch,
		"files":     files,
		"truncated": len(files) >= maxFiles || totalBytes >= maxBytes,
	}), nil
}

// splitFrontmatter takes raw .md content and returns the YAML
// frontmatter map plus the body. Empty frontmatter when none is
// present.
func splitFrontmatter(raw []byte) (map[string]any, string) {
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") {
		return nil, s
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return nil, s
	}
	fmText := s[4 : 4+end]
	body := s[4+end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		return nil, s
	}
	return fm, body
}

// ---------- agentboard_propose ----------

func (s *Server) toolPropose(r *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	ws := getString(args, "workspace")
	if ws == "" {
		return nil, &RPCError{Code: -32602, Message: "workspace required"}
	}
	message := getString(args, "message")
	if message == "" {
		return nil, &RPCError{Code: -32602, Message: "message required"}
	}
	if s.ProposeFn == nil {
		return nil, &RPCError{Code: -32000, Message: "propose path not wired (git substrate offline?)"}
	}
	rawFiles, ok := args["files"]
	if !ok {
		return nil, &RPCError{Code: -32602, Message: "files required"}
	}
	var fileList []struct {
		Path string  `json:"path"`
		Body *string `json:"body"`
	}
	if err := json.Unmarshal(rawFiles, &fileList); err != nil {
		return nil, &RPCError{Code: -32602, Message: "files: " + err.Error()}
	}
	if len(fileList) == 0 {
		return nil, &RPCError{Code: -32602, Message: "files must be non-empty"}
	}
	actor := s.resolveActor(r)
	branch := getString(args, "branch")
	base := getString(args, "base")
	changes := make([]ProposeFile, 0, len(fileList))
	for _, f := range fileList {
		changes = append(changes, ProposeFile{Path: f.Path, Body: f.Body})
	}
	res, err := s.ProposeFn(r.Context(), ProposeRequest{
		Workspace: ws,
		Base:      base,
		Branch:    branch,
		Message:   message,
		Actor:     actor,
		Files:     changes,
	})
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: "propose: " + err.Error()}
	}
	return mcpJSON(res), nil
}

// ProposeRequest, ProposeFile, ProposeResult are the wire shape between
// the MCP tool and the gitserver helper. Kept in this package so the
// gitserver doesn't have to depend on mcp.
type ProposeRequest struct {
	Workspace string
	Base      string
	Branch    string
	Message   string
	Actor     string
	Files     []ProposeFile
}

type ProposeFile struct {
	Path string  // workspace-relative
	Body *string // nil = delete
}

type ProposeResult struct {
	Success    bool     `json:"success"`
	Branch     string   `json:"branch"`
	Commit     string   `json:"commit,omitempty"`
	ProposalID string   `json:"proposal_id,omitempty"`
	Conflicts  []string `json:"conflicts,omitempty"`
	Message    string   `json:"message,omitempty"`
}

// ProposeFunc is the server-side implementation of agentboard_propose.
// Wired by cli/serve.go to a function that drives gitserver.
type ProposeFunc func(ctx context.Context, req ProposeRequest) (*ProposeResult, error)

// ---------- agentboard_resolve_conflict ----------

func (s *Server) toolResolveConflict(r *http.Request, args map[string]json.RawMessage) (any, *RPCError) {
	if s.ResolveConflictFn == nil {
		return nil, &RPCError{Code: -32000, Message: "resolve_conflict not wired (git substrate offline?)"}
	}
	proposal := getString(args, "proposal")
	file := getString(args, "file")
	resolution := getString(args, "resolution")
	if proposal == "" || file == "" {
		return nil, &RPCError{Code: -32602, Message: "proposal and file required"}
	}
	res, err := s.ResolveConflictFn(r.Context(), proposal, file, resolution)
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: "resolve_conflict: " + err.Error()}
	}
	return mcpJSON(res), nil
}

// ResolveConflictFunc is the server-side implementation of
// agentboard_resolve_conflict. cli/serve.go wires it to
// gitserver.Store.ResolveConflict.
type ResolveConflictFunc func(ctx context.Context, proposalID, file, resolution string) (*ProposeResult, error)

// (agentboard_subscribe + agentboard_fire_event are gone post-pivot.
// The event-bus surface they exposed was over-built for a wiki where
// humans and agents collaborate on shared pages. Agents poll or
// re-pull to discover peer changes; SSE on /_api/events handles the
// browser-side reload toast.)
//
// (agentboard_grab is gone in the substrate pivot. The materializer
// it backed walked a page-manager index that doesn't exist anymore.
// Agents pull whatever paths they need via agentboard_pull(workspace)
// — that's the bundle equivalent.)
