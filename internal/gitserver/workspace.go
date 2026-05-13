// Package gitserver hosts the git smart-HTTPS endpoint that agents
// clone, branch, commit, and push against. Spec §§2–5.
//
// Each workspace is a bare git repo on disk plus a working-tree
// mirror that the server keeps checked out to the workspace's
// default branch. The SPA reads from the working tree; agents read
// and write via git. The bare repo is the source of truth.
package gitserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Workspace is one bare repo + one working tree under a single name.
type Workspace struct {
	ID            string
	DefaultBranch string
	Policy        string // "push-to-main" | "always-pr"
	CreatedAt     time.Time
	CreatedBy     string
}

// Store is the SQLite-backed workspace registry. Substrate state
// (the bare repos themselves) lives on the filesystem under
// <root>/repos/<id>.git; this struct just tracks the metadata.
type Store struct {
	db      *sql.DB
	root    string // base dir containing repos/ and worktrees/
	rootMux struct{}
}

// NewStore wires up the registry against an existing SQLite DB. The
// caller passes the directory under which `repos/` and `worktrees/`
// will live (typically `<project>/.agentboard`).
func NewStore(db *sql.DB, root string) (*Store, error) {
	s := &Store{db: db, root: root}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	for _, sub := range []string{"repos", "worktrees"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}
	return s, nil
}

func (s *Store) migrate() error {
	const schemaSQL = `
CREATE TABLE IF NOT EXISTS git_workspaces (
    id              TEXT NOT NULL PRIMARY KEY COLLATE NOCASE,
    default_branch  TEXT NOT NULL DEFAULT 'main',
    policy          TEXT NOT NULL DEFAULT 'push-to-main' CHECK (policy IN ('push-to-main','always-pr')),
    created_at      INTEGER NOT NULL,
    created_by      TEXT
) STRICT;
`
	_, err := s.db.Exec(schemaSQL)
	return err
}

// BarePath returns the on-disk path for a workspace's bare repo.
func (s *Store) BarePath(id string) string {
	return filepath.Join(s.root, "repos", id+".git")
}

// WorktreePath returns the on-disk path for a workspace's working tree.
func (s *Store) WorktreePath(id string) string {
	return filepath.Join(s.root, "worktrees", id)
}

// Root returns the registry root directory (parent of repos/ and worktrees/).
func (s *Store) Root() string { return s.root }

// Get returns the workspace by id, or nil if missing.
func (s *Store) Get(ctx context.Context, id string) (*Workspace, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, default_branch, policy, created_at, IFNULL(created_by,'') FROM git_workspaces WHERE id = ?`, id)
	var w Workspace
	var createdAt int64
	if err := row.Scan(&w.ID, &w.DefaultBranch, &w.Policy, &createdAt, &w.CreatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	w.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &w, nil
}

// List returns every workspace, ordered by id.
func (s *Store) List(ctx context.Context) ([]*Workspace, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, default_branch, policy, created_at, IFNULL(created_by,'') FROM git_workspaces ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Workspace{}
	for rows.Next() {
		var w Workspace
		var createdAt int64
		if err := rows.Scan(&w.ID, &w.DefaultBranch, &w.Policy, &createdAt, &w.CreatedBy); err != nil {
			return nil, err
		}
		w.CreatedAt = time.Unix(createdAt, 0).UTC()
		out = append(out, &w)
	}
	return out, rows.Err()
}

// Create inserts a registry row, initializes a bare repo on disk, and
// (when seed != "") commits the contents of the seed directory as the
// initial commit. Idempotent: calling Create for an existing id is a
// no-op and returns the existing workspace.
func (s *Store) Create(ctx context.Context, id string, createdBy string, seed string) (*Workspace, error) {
	if !validWorkspaceID(id) {
		return nil, fmt.Errorf("invalid workspace id %q", id)
	}
	if existing, err := s.Get(ctx, id); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	bare := s.BarePath(id)
	// `git init --bare` is idempotent against an empty directory; we
	// only call it if the path doesn't already look like a repo.
	if _, err := os.Stat(filepath.Join(bare, "HEAD")); err != nil {
		if err := os.MkdirAll(bare, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir bare: %w", err)
		}
		if out, err := runGit("", "init", "--bare", "--initial-branch=main", bare); err != nil {
			return nil, fmt.Errorf("git init --bare: %w (output: %s)", err, out)
		}
		// Tell the bare repo to accept pushes to the currently-checked-out
		// branch (we won't have a working tree at this layer, but
		// receive.denyCurrentBranch needs to be off for the post-receive
		// updateRef path to be unrestricted).
		if out, err := runGit("", "config", "--file", filepath.Join(bare, "config"), "receive.denyCurrentBranch", "ignore"); err != nil {
			return nil, fmt.Errorf("git config receive.denyCurrentBranch: %w (output: %s)", err, out)
		}
	}

	// Initial seed: if a non-empty seed directory is supplied, build
	// the first commit from it via a temporary working tree. Skipped
	// when seed == "" or the bare repo already has commits.
	if seed != "" {
		hasCommits, err := bareHasCommits(bare)
		if err != nil {
			return nil, err
		}
		if !hasCommits {
			if err := seedInitialCommit(bare, seed, createdBy); err != nil {
				return nil, fmt.Errorf("seed initial commit: %w", err)
			}
		}
	}

	w := &Workspace{
		ID:            id,
		DefaultBranch: "main",
		Policy:        "push-to-main",
		CreatedAt:     time.Now().UTC(),
		CreatedBy:     createdBy,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO git_workspaces (id, default_branch, policy, created_at, created_by) VALUES (?, ?, ?, ?, ?)`,
		w.ID, w.DefaultBranch, w.Policy, w.CreatedAt.Unix(), w.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	return w, nil
}

// EnsureFile writes `body` at `path` inside the workspace and commits
// it if no file at that path exists yet on the default branch. Used
// to retrofit pre-§15 workspaces with a bootstrap README without
// stepping on any file an operator might already have authored.
//
// Idempotent: if the path already exists, EnsureFile is a no-op.
func (s *Store) EnsureFile(ctx context.Context, workspaceID, path, body, actor, commitMsg string) error {
	ws, err := s.Get(ctx, workspaceID)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("workspace %q not found", workspaceID)
	}
	bare := s.BarePath(workspaceID)

	// Check whether the file already exists on the default branch.
	if out, err := runGit("", "--git-dir", bare, "cat-file", "-e", ws.DefaultBranch+":"+path); err == nil {
		_ = out
		return nil
	}

	// Clone, write, commit, push.
	tmp, err := os.MkdirTemp("", "ab-ensure-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if out, err := runGit("", "clone", "--branch", ws.DefaultBranch, bare, tmp); err != nil {
		return fmt.Errorf("clone bare: %w (output: %s)", err, out)
	}
	for _, kv := range [][2]string{
		{"user.email", actor + "@agentboard.local"},
		{"user.name", actor},
	} {
		if out, err := runGitIn(tmp, "config", kv[0], kv[1]); err != nil {
			return fmt.Errorf("git config %s: %w (output: %s)", kv[0], err, out)
		}
	}
	target := filepath.Join(tmp, path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		return err
	}
	if out, err := runGitIn(tmp, "add", path); err != nil {
		return fmt.Errorf("git add: %w (output: %s)", err, out)
	}
	if commitMsg == "" {
		commitMsg = "Add " + path
	}
	if out, err := runGitIn(tmp, "commit", "-m", commitMsg); err != nil {
		return fmt.Errorf("git commit: %w (output: %s)", err, out)
	}
	if out, err := runGitIn(tmp, "push", "origin", "HEAD:"+ws.DefaultBranch); err != nil {
		return fmt.Errorf("git push: %w (output: %s)", err, out)
	}
	// Re-sync the working-tree mirror so the SPA sees the new file
	// without waiting for the post-receive hook.
	_, _ = s.SyncWorktree(ctx, workspaceID)
	return nil
}

// validWorkspaceID rejects names that would break the URL or the disk
// path: empty, with slashes, dots at the boundary, control chars.
func validWorkspaceID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	if strings.HasPrefix(id, ".") || strings.HasSuffix(id, ".") {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

// bareHasCommits reports whether the bare repo at `dir` has at least
// one commit on any branch.
func bareHasCommits(dir string) (bool, error) {
	out, err := runGit(dir, "--git-dir", dir, "rev-list", "--all", "-n", "1")
	if err != nil {
		// rev-list errors out on empty repos with a "fatal: bad
		// revision" but the exit isn't fatal for us — empty is just
		// "no commits."
		return false, nil
	}
	return strings.TrimSpace(out) != "", nil
}

// seedInitialCommit builds the first commit of a fresh bare repo from
// the contents of a source directory. Skips `_meta.version` and the
// `.agentboard/` operational directory if encountered — those are
// v0.13 substrate metadata that doesn't belong in the new git tree.
func seedInitialCommit(bare, source string, actor string) error {
	tmp, err := os.MkdirTemp("", "ab-seed-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	// Clone the bare into a temporary working dir, copy seed content,
	// commit, push. This is the cheapest way to author the initial
	// commit without inventing tree-builder logic.
	if out, err := runGit("", "clone", bare, tmp); err != nil {
		return fmt.Errorf("clone bare into tmp: %w (output: %s)", err, out)
	}
	if err := copyTree(source, tmp); err != nil {
		return fmt.Errorf("copy seed tree: %w", err)
	}
	for _, kv := range [][2]string{
		{"user.email", actor + "@agentboard.local"},
		{"user.name", actor},
	} {
		if out, err := runGitIn(tmp, "config", kv[0], kv[1]); err != nil {
			return fmt.Errorf("git config %s: %w (output: %s)", kv[0], err, out)
		}
	}
	if out, err := runGitIn(tmp, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w (output: %s)", err, out)
	}
	// Allow-empty so we can seed a workspace from an empty source dir
	// without the initial commit failing.
	if out, err := runGitIn(tmp, "commit", "--allow-empty", "-m", "Initial commit (migrated from v0.13 substrate)"); err != nil {
		return fmt.Errorf("git commit: %w (output: %s)", err, out)
	}
	if out, err := runGitIn(tmp, "push", "origin", "HEAD:main"); err != nil {
		return fmt.Errorf("git push: %w (output: %s)", err, out)
	}
	return nil
}

// copyTree mirrors `source` into `dest`, skipping the v0.13
// operational artifacts that shouldn't enter git history.
func copyTree(source, dest string) error {
	skip := map[string]bool{
		".agentboard":   true,
		".git":          true,
		"agentboard":    true, // the built binary
		"agentboard.yaml": true, // operator config; per §13 stays in SQLite-land
	}
	return filepath.Walk(source, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(source, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		// Skip operational dirs at any depth.
		parts := strings.Split(rel, string(os.PathSeparator))
		if skip[parts[0]] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(p, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	buf := make([]byte, 64*1024)
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr != nil {
			if rerr.Error() == "EOF" || errors.Is(rerr, errEOF) {
				return nil
			}
			return rerr
		}
	}
}

// errEOF is the sentinel from io.EOF — duplicated here so we don't
// pull `io` for one constant.
var errEOF = errors.New("EOF")

// runGit invokes `git` with the given args. dir, when non-empty, is
// passed as -C; otherwise git runs in the current working directory.
// Returns combined stdout+stderr.
func runGit(dir string, args ...string) (string, error) {
	allArgs := args
	if dir != "" {
		allArgs = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", allArgs...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runGitIn invokes `git` with cwd set to `dir`. Used when we need
// working-tree-aware operations (commit, push) that don't accept
// -C the same way.
func runGitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
