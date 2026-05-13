package gitserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProposeFile is one change in a proposal.
type ProposeFile struct {
	Path string
	Body *string // nil = delete
}

// ProposeRequest is the server-side propose API. Mirrors the MCP
// tool shape but lives here so the gitserver doesn't depend on mcp.
type ProposeRequest struct {
	Workspace string
	Base      string
	Branch    string
	Message   string
	Actor     string
	Files     []ProposeFile
}

// ProposeResult is what Propose returns. Conflicts is populated when
// the push failed because main moved underneath us.
type ProposeResult struct {
	Success   bool     `json:"success"`
	Branch    string   `json:"branch"`
	Commit    string   `json:"commit,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`
	Message   string   `json:"message,omitempty"`
}

// Propose runs the server-side commit + push. Used by
// agentboard_propose for git-less agents. The flow:
//
//   1. Clone the bare repo into a temp directory.
//   2. Check out `base` (or the workspace default if empty) and
//      create the proposal branch (`branch` if supplied, else
//      `proposal/<random>`).
//   3. Write the files (or delete when body is nil), `git add -A`,
//      `git commit -m message`.
//   4. Push to the workspace's bare repo:
//        - push-to-main policy → push HEAD:main directly.
//        - always-pr (future cut) → push to the proposal branch.
//   5. Translate any push rejection into a Conflicts list and return
//      it for the caller to resolve.
func (s *Store) Propose(ctx context.Context, req ProposeRequest) (*ProposeResult, error) {
	if req.Workspace == "" {
		return nil, fmt.Errorf("workspace required")
	}
	ws, err := s.Get(ctx, req.Workspace)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", req.Workspace)
	}
	bare := s.BarePath(req.Workspace)
	base := req.Base
	if base == "" {
		base = ws.DefaultBranch
	}

	tmp, err := os.MkdirTemp("", "ab-propose-*")
	if err != nil {
		return nil, fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(tmp)

	if out, err := runGit("", "clone", "--branch", base, bare, tmp); err != nil {
		return nil, fmt.Errorf("clone bare: %w (output: %s)", err, out)
	}
	for _, kv := range [][2]string{
		{"user.email", req.Actor + "@agentboard.local"},
		{"user.name", req.Actor},
	} {
		if out, err := runGitIn(tmp, "config", kv[0], kv[1]); err != nil {
			return nil, fmt.Errorf("git config %s: %w (output: %s)", kv[0], err, out)
		}
	}

	for _, f := range req.Files {
		clean := filepath.Clean(f.Path)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) || strings.HasPrefix(clean, "/") {
			return nil, fmt.Errorf("invalid path %q", f.Path)
		}
		target := filepath.Join(tmp, clean)
		if f.Body == nil {
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("remove %s: %w", clean, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir for %s: %w", clean, err)
		}
		if err := os.WriteFile(target, []byte(*f.Body), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", clean, err)
		}
	}

	if out, err := runGitIn(tmp, "add", "-A"); err != nil {
		return nil, fmt.Errorf("git add: %w (output: %s)", err, out)
	}

	// Allow-empty so a no-op proposal still completes cleanly. Useful
	// for "just bump main to a known state" calls.
	commitMsg := req.Message
	if commitMsg == "" {
		commitMsg = "propose"
	}
	if out, err := runGitIn(tmp, "commit", "--allow-empty", "-m", commitMsg); err != nil {
		return nil, fmt.Errorf("git commit: %w (output: %s)", err, out)
	}

	// Get the new commit SHA for the result.
	commit := ""
	if sha, _ := runGitIn(tmp, "rev-parse", "HEAD"); sha != "" {
		commit = strings.TrimSpace(sha)
	}

	// Decide the destination ref. push-to-main is the only supported
	// policy today; always-pr lands in Cut 6 by changing the dest
	// branch + writing a proposals row.
	destBranch := "main"
	if ws.Policy == "always-pr" {
		// Fallback for the always-pr stub: route to a proposal branch.
		// The proposal merge tooling lands in Cut 6.
		destBranch = req.Branch
		if destBranch == "" {
			destBranch = fmt.Sprintf("proposal/%s/%s", req.Actor, commit[:8])
		}
	}

	pushOut, pushErr := runGitIn(tmp, "push", "origin", "HEAD:"+destBranch)
	if pushErr != nil {
		// Detect non-fast-forward / rejected push and surface a
		// useful conflicts hint. For now we don't list files; the
		// caller pulls and re-resolves.
		if strings.Contains(pushOut, "non-fast-forward") || strings.Contains(pushOut, "rejected") {
			return &ProposeResult{
				Success: false,
				Branch:  destBranch,
				Message: "Push rejected: someone else pushed first. Pull, resolve, retry.",
			}, nil
		}
		return nil, fmt.Errorf("git push: %w (output: %s)", pushErr, pushOut)
	}

	// Re-checkout the working tree mirror so the SPA picks up the
	// change. We could also rely on the push hook the HTTP path
	// fires; calling here directly is harmless and avoids the SPA
	// briefly serving stale content between push completion and
	// hook execution.
	if _, err := s.SyncWorktree(ctx, req.Workspace); err != nil {
		// non-fatal: the push hook still runs
	}

	return &ProposeResult{
		Success: true,
		Branch:  destBranch,
		Commit:  commit,
	}, nil
}
