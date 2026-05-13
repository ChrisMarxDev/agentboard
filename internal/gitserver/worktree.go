package gitserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// SyncWorktree re-checks out the workspace's default branch into its
// working-tree directory. Called from the push hook so a successful
// push immediately materializes into the SPA-readable tree.
//
// We use `git --git-dir=<bare> --work-tree=<wt> checkout -f <branch>`
// which:
//   - is idempotent if no refs moved,
//   - overwrites whatever is in the working tree (the v0.13 SPA
//     watcher's only authority on tree state is "what's on disk
//     right now"; we re-establish that authority from the bare repo).
//
// Returns the absolute path of the working tree on success so the
// caller can plug it into the file watcher.
func (s *Store) SyncWorktree(ctx context.Context, workspaceID string) (string, error) {
	ws, err := s.Get(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	if ws == nil {
		return "", fmt.Errorf("workspace %q not found", workspaceID)
	}
	bare := s.BarePath(workspaceID)
	wt := s.WorktreePath(workspaceID)
	if err := os.MkdirAll(wt, 0o755); err != nil {
		return "", fmt.Errorf("mkdir worktree: %w", err)
	}

	args := []string{
		"--git-dir", bare,
		"--work-tree", wt,
		"checkout", "-f", ws.DefaultBranch,
	}
	if out, err := runGit("", args...); err != nil {
		// First-run case: the index for the working tree doesn't exist
		// yet. `read-tree` builds one from the default branch tip, then
		// retry checkout.
		if _, rerr := runGit("", "--git-dir", bare, "--work-tree", wt, "read-tree", ws.DefaultBranch); rerr != nil {
			return "", fmt.Errorf("git read-tree: %w (output: %s)", rerr, out)
		}
		if out2, err2 := runGit("", args...); err2 != nil {
			return "", fmt.Errorf("git checkout: %w (output: %s)", err2, out2)
		}
	}
	return filepath.Clean(wt), nil
}

// EnsureWorktree calls SyncWorktree if the working tree is missing
// or empty. Idempotent. Useful at server startup to materialize a
// freshly-created or freshly-loaded workspace.
func (s *Store) EnsureWorktree(ctx context.Context, workspaceID string) (string, error) {
	wt := s.WorktreePath(workspaceID)
	if entries, err := os.ReadDir(wt); err == nil && len(entries) > 0 {
		// Already has content — trust it. SyncWorktree on every push
		// re-establishes truth.
		return filepath.Clean(wt), nil
	}
	return s.SyncWorktree(ctx, workspaceID)
}
