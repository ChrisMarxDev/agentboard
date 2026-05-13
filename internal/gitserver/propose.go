package gitserver

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// ProposeResult is what Propose returns. Conflicts (when non-empty)
// is a list of repo-relative paths the caller must resolve via
// agentboard_resolve_conflict; the server keeps the in-progress merge
// alive in a persisted work_dir until then. ProposalID is the handle
// for those resolve_conflict calls.
type ProposeResult struct {
	Success    bool     `json:"success"`
	Branch     string   `json:"branch"`
	Commit     string   `json:"commit,omitempty"`
	ProposalID string   `json:"proposal_id,omitempty"`
	Conflicts  []string `json:"conflicts,omitempty"`
	Message    string   `json:"message,omitempty"`
}

// proposalRow is the on-disk representation. Lives in the
// `git_proposals` SQLite table the workspace store owns.
type proposalRow struct {
	ID         string
	Workspace  string
	BaseRef    string
	DestBranch string
	WorkDir    string
	Message    string
	Actor      string
	Conflicts  []string // remaining conflicted paths
	Status     string   // "pending" | "merged" | "abandoned"
	CreatedAt  time.Time
}

// proposalsMigrate creates the proposals table. Called from
// NewStore alongside the workspace registry migrate.
func (s *Store) proposalsMigrate() error {
	const schemaSQL = `
CREATE TABLE IF NOT EXISTS git_proposals (
    id              TEXT NOT NULL PRIMARY KEY,
    workspace       TEXT NOT NULL,
    base_ref        TEXT NOT NULL,
    dest_branch     TEXT NOT NULL,
    work_dir        TEXT NOT NULL,
    message         TEXT NOT NULL,
    actor           TEXT NOT NULL,
    conflicts_json  TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('pending','merged','abandoned')),
    created_at      INTEGER NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_git_proposals_workspace ON git_proposals(workspace, status);
`
	_, err := s.db.Exec(schemaSQL)
	return err
}

func (s *Store) saveProposal(ctx context.Context, p *proposalRow) error {
	conflicts, _ := json.Marshal(p.Conflicts)
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO git_proposals
			(id, workspace, base_ref, dest_branch, work_dir, message, actor, conflicts_json, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Workspace, p.BaseRef, p.DestBranch, p.WorkDir, p.Message, p.Actor,
		string(conflicts), p.Status, p.CreatedAt.Unix())
	return err
}

func (s *Store) loadProposal(ctx context.Context, id string) (*proposalRow, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, workspace, base_ref, dest_branch, work_dir, message, actor,
		        conflicts_json, status, created_at
		 FROM git_proposals WHERE id = ?`, id)
	var p proposalRow
	var conflictsJSON string
	var createdAt int64
	if err := row.Scan(&p.ID, &p.Workspace, &p.BaseRef, &p.DestBranch, &p.WorkDir,
		&p.Message, &p.Actor, &conflictsJSON, &p.Status, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(conflictsJSON), &p.Conflicts)
	p.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &p, nil
}

// Propose runs the server-side commit + push. Used by
// agentboard_propose for git-less agents.
//
// Happy path: clone the bare repo to a temp dir, apply files, commit,
// fast-forward push. Done.
//
// Conflict path: fetch + merge against the target ref. If the merge
// has unresolved markers, the conflicted files are recorded in the
// git_proposals SQLite table along with the work_dir's location. The
// caller iterates ResolveConflict per file; once the remaining
// conflicts list is empty, ResolveConflict commits the merge and
// retries the push.
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

	// Choose a stable proposals dir under the workspace store root so
	// state survives a server restart. Caller code in cli/serve.go
	// can optionally GC stale proposals at boot.
	proposalsDir := filepath.Join(s.root, "proposals")
	if err := os.MkdirAll(proposalsDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir proposals: %w", err)
	}
	id := randID()
	work := filepath.Join(proposalsDir, id)

	if out, err := runGit("", "clone", "--branch", base, bare, work); err != nil {
		os.RemoveAll(work)
		return nil, fmt.Errorf("clone bare: %w (output: %s)", err, out)
	}

	if err := configActor(work, req.Actor); err != nil {
		os.RemoveAll(work)
		return nil, err
	}

	if err := applyProposeFiles(work, req.Files); err != nil {
		os.RemoveAll(work)
		return nil, err
	}

	if out, err := runGitIn(work, "add", "-A"); err != nil {
		os.RemoveAll(work)
		return nil, fmt.Errorf("git add: %w (output: %s)", err, out)
	}

	commitMsg := req.Message
	if commitMsg == "" {
		commitMsg = "propose"
	}
	if out, err := runGitIn(work, "commit", "--allow-empty", "-m", commitMsg); err != nil {
		os.RemoveAll(work)
		return nil, fmt.Errorf("git commit: %w (output: %s)", err, out)
	}
	commit := headSHA(work)

	destBranch := "main"
	if ws.Policy == "always-pr" {
		destBranch = req.Branch
		if destBranch == "" {
			destBranch = fmt.Sprintf("proposal/%s/%s", req.Actor, safeShortSHA(commit))
		}
	}

	// First push attempt — fast-forward.
	pushOut, pushErr := runGitIn(work, "push", "origin", "HEAD:"+destBranch)
	if pushErr == nil {
		// Clean win.
		os.RemoveAll(work)
		_, _ = s.SyncWorktree(ctx, req.Workspace)
		s.fireInternalPush(ctx, req.Workspace)
		return &ProposeResult{Success: true, Branch: destBranch, Commit: commit}, nil
	}
	if !isPushRejected(pushOut) {
		os.RemoveAll(work)
		return nil, fmt.Errorf("git push: %w (output: %s)", pushErr, pushOut)
	}

	// Conflict path: fetch the target branch, merge into our branch,
	// surface the conflicted files. The merge is left half-done in
	// the work_dir; resolve_conflict will finish it.
	if out, err := runGitIn(work, "fetch", "origin", destBranch); err != nil {
		os.RemoveAll(work)
		return nil, fmt.Errorf("git fetch after rejection: %w (output: %s)", err, out)
	}
	mergeOut, _ := runGitIn(work, "merge", "--no-edit", "--no-ff", "FETCH_HEAD")
	conflicts := listConflicts(work)
	if len(conflicts) == 0 {
		// Merge succeeded automatically (the original push raced but the
		// content didn't conflict). Retry push.
		retryOut, retryErr := runGitIn(work, "push", "origin", "HEAD:"+destBranch)
		if retryErr == nil {
			sha := headSHA(work)
			os.RemoveAll(work)
			_, _ = s.SyncWorktree(ctx, req.Workspace)
			s.fireInternalPush(ctx, req.Workspace)
			return &ProposeResult{Success: true, Branch: destBranch, Commit: sha}, nil
		}
		// Couldn't push even after clean merge — return as opaque
		// "unmergable" error with the captured merge output for the
		// caller's diagnosis.
		os.RemoveAll(work)
		return nil, fmt.Errorf("git push after clean merge: %w (merge: %s) (push: %s)", retryErr, mergeOut, retryOut)
	}

	// Real conflicts. Persist the proposal so the caller can resolve
	// them iteratively.
	row := &proposalRow{
		ID:         id,
		Workspace:  req.Workspace,
		BaseRef:    base,
		DestBranch: destBranch,
		WorkDir:    work,
		Message:    commitMsg,
		Actor:      req.Actor,
		Conflicts:  conflicts,
		Status:     "pending",
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.saveProposal(ctx, row); err != nil {
		os.RemoveAll(work)
		return nil, fmt.Errorf("save proposal: %w", err)
	}
	return &ProposeResult{
		Success:    false,
		Branch:     destBranch,
		ProposalID: id,
		Conflicts:  conflicts,
		Message:    "Push rejected and the merge has text conflicts. Call agentboard_resolve_conflict(proposal_id, file, resolution) for each path in `conflicts` until the list is empty; the server retries the push when the last conflict resolves.",
	}, nil
}

// ResolveConflict applies a resolution for one conflicted file in a
// pending proposal. When the proposal's remaining-conflicts list is
// empty after this call, the server commits the merge and retries
// the push. Returns the updated proposal state.
func (s *Store) ResolveConflict(ctx context.Context, proposalID, file, resolution string) (*ProposeResult, error) {
	if proposalID == "" {
		return nil, fmt.Errorf("proposal_id required")
	}
	if file == "" {
		return nil, fmt.Errorf("file required")
	}
	p, err := s.loadProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("proposal %q not found", proposalID)
	}
	if p.Status != "pending" {
		return nil, fmt.Errorf("proposal %q is %s, not pending", proposalID, p.Status)
	}

	// Confirm the file is actually in the remaining conflicts list.
	idx := -1
	for i, f := range p.Conflicts {
		if f == file {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("file %q is not in this proposal's remaining conflicts: %v", file, p.Conflicts)
	}

	// Write the resolved bytes, stage the file, drop it from the list.
	clean := filepath.Clean(file)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) || strings.HasPrefix(clean, "/") {
		return nil, fmt.Errorf("invalid path %q", file)
	}
	target := filepath.Join(p.WorkDir, clean)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(target, []byte(resolution), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", file, err)
	}
	if out, err := runGitIn(p.WorkDir, "add", file); err != nil {
		return nil, fmt.Errorf("git add: %w (output: %s)", err, out)
	}

	p.Conflicts = append(p.Conflicts[:idx], p.Conflicts[idx+1:]...)

	// More conflicts remain → persist and return.
	if len(p.Conflicts) > 0 {
		if err := s.saveProposal(ctx, p); err != nil {
			return nil, err
		}
		return &ProposeResult{
			Success:    false,
			Branch:     p.DestBranch,
			ProposalID: p.ID,
			Conflicts:  p.Conflicts,
			Message:    fmt.Sprintf("%d file(s) still conflicted; call resolve_conflict again for each.", len(p.Conflicts)),
		}, nil
	}

	// Last conflict resolved — finalize the merge commit and push.
	if out, err := runGitIn(p.WorkDir, "commit", "--no-edit"); err != nil {
		return nil, fmt.Errorf("git commit merge: %w (output: %s)", err, out)
	}
	pushOut, pushErr := runGitIn(p.WorkDir, "push", "origin", "HEAD:"+p.DestBranch)
	if pushErr != nil {
		if isPushRejected(pushOut) {
			// Someone pushed AGAIN while we were resolving. Re-merge
			// against the new tip and repopulate conflicts. Caller
			// keeps iterating.
			if out, ferr := runGitIn(p.WorkDir, "fetch", "origin", p.DestBranch); ferr != nil {
				return nil, fmt.Errorf("git fetch on second rejection: %w (output: %s)", ferr, out)
			}
			_, _ = runGitIn(p.WorkDir, "merge", "--no-edit", "--no-ff", "FETCH_HEAD")
			newConflicts := listConflicts(p.WorkDir)
			p.Conflicts = newConflicts
			if err := s.saveProposal(ctx, p); err != nil {
				return nil, err
			}
			if len(newConflicts) == 0 {
				// Re-merge clean; try push once more.
				if out, err := runGitIn(p.WorkDir, "push", "origin", "HEAD:"+p.DestBranch); err == nil {
					_ = out
					return finalizeProposal(s, ctx, p)
				}
			}
			return &ProposeResult{
				Success:    false,
				Branch:     p.DestBranch,
				ProposalID: p.ID,
				Conflicts:  newConflicts,
				Message:    "Push rejected again while resolving — fresh conflicts attached. Re-resolve.",
			}, nil
		}
		return nil, fmt.Errorf("git push: %w (output: %s)", pushErr, pushOut)
	}
	return finalizeProposal(s, ctx, p)
}

func finalizeProposal(s *Store, ctx context.Context, p *proposalRow) (*ProposeResult, error) {
	commit := headSHA(p.WorkDir)
	p.Status = "merged"
	_ = s.saveProposal(ctx, p)
	os.RemoveAll(p.WorkDir)
	_, _ = s.SyncWorktree(ctx, p.Workspace)
	s.fireInternalPush(ctx, p.Workspace)
	return &ProposeResult{
		Success: true,
		Branch:  p.DestBranch,
		Commit:  commit,
	}, nil
}

// fireInternalPush invokes the OnInternalPush callback if set. The
// caller passes the context that owns the originating request. No-op
// when the callback isn't wired (early init, tests).
func (s *Store) fireInternalPush(ctx context.Context, workspace string) {
	if s.OnInternalPush == nil {
		return
	}
	s.OnInternalPush(ctx, workspace)
}

// GCStaleProposals removes proposals older than `maxAge` whose status
// is still pending. Called at server boot to prevent crash-leftover
// work dirs from accumulating. Safe to run repeatedly.
func (s *Store) GCStaleProposals(ctx context.Context, maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge).Unix()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, work_dir FROM git_proposals
		 WHERE status = 'pending' AND created_at < ?`, cutoff)
	if err != nil {
		return err
	}
	defer rows.Close()
	type stale struct{ id, dir string }
	var staleRows []stale
	for rows.Next() {
		var s stale
		if err := rows.Scan(&s.id, &s.dir); err != nil {
			return err
		}
		staleRows = append(staleRows, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, row := range staleRows {
		_ = os.RemoveAll(row.dir)
		_, _ = s.db.ExecContext(ctx,
			`UPDATE git_proposals SET status = 'abandoned' WHERE id = ?`, row.id)
	}
	return nil
}

// --- helpers ---

func configActor(dir, actor string) error {
	if actor == "" {
		actor = "agent"
	}
	for _, kv := range [][2]string{
		{"user.email", actor + "@agentboard.local"},
		{"user.name", actor},
	} {
		if out, err := runGitIn(dir, "config", kv[0], kv[1]); err != nil {
			return fmt.Errorf("git config %s: %w (output: %s)", kv[0], err, out)
		}
	}
	return nil
}

func applyProposeFiles(workDir string, files []ProposeFile) error {
	for _, f := range files {
		clean := filepath.Clean(f.Path)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) || strings.HasPrefix(clean, "/") {
			return fmt.Errorf("invalid path %q", f.Path)
		}
		target := filepath.Join(workDir, clean)
		if f.Body == nil {
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove %s: %w", clean, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", clean, err)
		}
		if err := os.WriteFile(target, []byte(*f.Body), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", clean, err)
		}
	}
	return nil
}

func headSHA(work string) string {
	sha, _ := runGitIn(work, "rev-parse", "HEAD")
	return strings.TrimSpace(sha)
}

func safeShortSHA(sha string) string {
	if len(sha) >= 8 {
		return sha[:8]
	}
	if sha == "" {
		return randID()[:8]
	}
	return sha
}

func isPushRejected(out string) bool {
	return strings.Contains(out, "non-fast-forward") ||
		strings.Contains(out, "rejected") ||
		strings.Contains(out, "fetch first")
}

// listConflicts returns the repo-relative paths of files in the
// conflict state (`git status --porcelain` codes UU, AA, DU, UD, etc.
// — any code starting with U/A on either side indicates an
// unresolved merge).
func listConflicts(work string) []string {
	out, err := runGitIn(work, "status", "--porcelain")
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		x, y := line[0], line[1]
		// Conflict markers per git-status(1) §"Short Format":
		// DD, AU, UD, UA, DU, AA, UU.
		conflicting := (x == 'U' || y == 'U') ||
			(x == 'A' && y == 'A') ||
			(x == 'D' && y == 'D')
		if !conflicting {
			continue
		}
		paths = append(paths, strings.TrimSpace(line[3:]))
	}
	return paths
}

func randID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
