package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	dbpkg "github.com/christophermarx/agentboard/internal/db"
	embedpkg "github.com/christophermarx/agentboard/internal/embed"
	"github.com/christophermarx/agentboard/internal/gitserver"
	"github.com/christophermarx/agentboard/internal/groups"
	htmlserver "github.com/christophermarx/agentboard/internal/html"
	"github.com/christophermarx/agentboard/internal/invitations"
	"github.com/christophermarx/agentboard/internal/mcp"
	"github.com/christophermarx/agentboard/internal/permissions"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/christophermarx/agentboard/internal/search"
	"github.com/christophermarx/agentboard/internal/server"
	"github.com/spf13/cobra"
)

// htmlEscape is a tiny alias around html.EscapeString — used in
// search snippet rendering. Pulled into a local name to keep the
// inline call sites short.
var htmlEscape = html.EscapeString

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the AgentBoard server",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't open browser on startup")
	serveCmd.Flags().StringVar(&authToken, "auth-token", "", "Shared token required for every request (except /_api/health). Falls back to AGENTBOARD_AUTH_TOKEN env var.")
}

func resolveProjectPath() string {
	if projectPath != "" {
		return projectPath
	}
	if env := os.Getenv("AGENTBOARD_PATH"); env != "" {
		return env
	}
	if projectName != "" {
		return project.NamedProjectDir(projectName)
	}
	if env := os.Getenv("AGENTBOARD_PROJECT"); env != "" {
		return project.NamedProjectDir(env)
	}
	return project.DefaultProjectDir()
}

func runServe(cmd *cobra.Command, args []string) error {
	projPath := resolveProjectPath()

	// Bootstrap the project dir on first boot. The new substrate has
	// no v0.13 "content/" tree; the agent's worktree IS the workspace.
	proj, err := project.Load(projPath)
	if err != nil {
		log.Printf("Initializing project at %s", projPath)
		proj, err = project.InitProject(projPath)
		if err != nil {
			return fmt.Errorf("init project: %w", err)
		}
	}
	if err := proj.EnsureDirs(); err != nil {
		return fmt.Errorf("ensure dirs: %w", err)
	}

	port := resolvePort()
	if port == 0 {
		port = proj.Config.Port
	}
	if port == 0 {
		port = 3000
	}

	dbConn, err := dbpkg.Open(proj.DatabasePath())
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer dbConn.Close()

	authStore, err := auth.NewStore(dbConn.Conn())
	if err != nil {
		return fmt.Errorf("open auth store: %w", err)
	}
	invStore, err := invitations.NewStore(dbConn.Conn())
	if err != nil {
		return fmt.Errorf("open invitations store: %w", err)
	}
	groupStore, err := groups.NewStore(dbConn.Conn())
	if err != nil {
		return fmt.Errorf("open groups store: %w", err)
	}

	// Bootstrap-admin invite on first boot.
	legacyToken := authToken
	if legacyToken == "" {
		legacyToken = os.Getenv("AGENTBOARD_AUTH_TOKEN")
	}
	bootstrapInv, err := authStore.BootstrapFirstAdmin(invStore, legacyToken, 0, log.Default())
	if err != nil {
		return fmt.Errorf("auth bootstrap: %w", err)
	}
	if bootstrapInv != nil {
		inviteURL := fmt.Sprintf("http://localhost:%d/invite/%s", port, bootstrapInv.ID)
		inviteFile := filepath.Join(proj.Path, ".agentboard", "first-admin-invite.url")
		_ = os.WriteFile(inviteFile, []byte(inviteURL+"\n"), 0o600)
		log.Printf("")
		log.Printf("  ==> Board is unclaimed. Open this URL to create the first admin:")
		log.Printf("      %s", inviteURL)
		log.Printf("      (also written to %s)", inviteFile)
		log.Printf("")
	}

	// Git substrate — bare repos + worktree mirrors live under
	// <project>/.agentboard/. spec-filesystem-substrate.md §§1–3.
	gitRoot := filepath.Join(proj.Path, ".agentboard")
	gitStore, err := gitserver.NewStore(dbConn.Conn(), gitRoot)
	if err != nil {
		return fmt.Errorf("open git workspace store: %w", err)
	}
	// FTS5 search index — rebuilt on each push.
	searchStore, err := search.NewStore(dbConn.Conn())
	if err != nil {
		return fmt.Errorf("open search store: %w", err)
	}
	// Seed the default workspace. Empty seed → empty workspace.
	if _, err := gitStore.Create(context.Background(), "dogfood", "system", ""); err != nil {
		log.Printf("Warning: could not initialize dogfood workspace: %v", err)
	}
	// Bootstrap README + SKILL into the workspace (idempotent).
	if err := gitStore.EnsureFile(context.Background(), "dogfood",
		"README.md", project.BootstrapReadmeMd, "system",
		"Add bootstrap README (CORE_GUIDELINES §15)"); err != nil {
		log.Printf("Warning: could not seed bootstrap README: %v", err)
	}
	// One canonical SKILL.md at the workspace root. Agent tools that
	// have a skill-loading convention (Claude reads .claude/skills/…,
	// Codex reads .codex/skills/…, etc.) can symlink or mirror this
	// file into their own tool-home — we don't pre-fork it into n
	// directories. The board itself doesn't special-case the path; it's
	// just markdown at the root, like the README.
	//
	// PutFile (content-idempotent) rather than EnsureFile (presence-
	// idempotent): the SKILL is the agent contract, and contract drift
	// is a bug. Workspaces that need a custom SKILL should commit it as
	// `MY-SKILL.md` or similar instead of editing the canonical file.
	if err := gitStore.PutFile(context.Background(), "dogfood",
		"SKILL.md", project.SeededRootSkill, "system",
		"Sync canonical SKILL"); err != nil {
		log.Printf("Warning: could not seed SKILL.md: %v", err)
	}
	// Retire the legacy skill paths from any previously-seeded board.
	for _, legacy := range []string{
		"skills/agentboard/SKILL.md",
		"skills/agentboard/examples.md",
		".claude/skills/agentboard/SKILL.md",
		".claude/skills/agentboard/examples.md",
	} {
		if err := gitStore.EnsureAbsent(context.Background(), "dogfood",
			legacy, "system",
			"Retire legacy skill path (single SKILL.md at root now)"); err != nil {
			log.Printf("Warning: could not retire %s: %v", legacy, err)
		}
	}
	// Demo content. HTML is the primary expressive primitive
	// (spec-filesystem-substrate.md); markdown stays for the
	// conventional surfaces (README, SKILL.md). Typed JSON for the
	// PutFile is content-idempotent: same body = no commit,
	// different body = a single update commit. The dogfood board is
	// the public showcase, so keeping the seeds current here is the
	// point. (Agents fork demo content into their own paths.)
	for _, seed := range []struct{ path, body, msg string }{
		{"index.html", project.SeededIndexHTML, "Update home page"},
		{"pages/getting-started.html", project.SeededGettingStartedHTML, "Update getting-started"},
		{"pages/changelog.html", project.SeededChangelogHTML, "Update changelog"},
		{"pages/roadmap.html", project.SeededRoadmapHTML, "Update roadmap"},
		{"pages/file-types.html", project.SeededFilesDemoHTML, "Demo: file types"},
		{"assets/logo.svg", project.SeededLogoSVG, "Add demo logo (SVG)"},
		{"assets/chart.svg", project.SeededChartSVG, "Add bar-chart demo (SVG)"},
		{"assets/flow.svg", project.SeededFlowSVG, "Add flow-diagram demo (SVG)"},
		{"assets/avatar.svg", project.SeededAvatarSVG, "Add avatar placeholder (SVG)"},
		{"data/sprint-14.csv", project.SeededSampleCSV, "Add sample CSV"},
		{"data/scratch.txt", project.SeededSampleTXT, "Add sample plain-text note"},
	} {
		if err := gitStore.PutFile(context.Background(), "dogfood",
			seed.path, seed.body, "system", seed.msg); err != nil {
			log.Printf("Warning: could not seed %s: %v", seed.path, err)
		}
	}

	// Retire legacy .md seeds from boards that received the earlier cut.
	// No-op on workspaces that never had them.
	for _, legacy := range []string{
		"index.md",
		"pages/getting-started.md",
		"pages/changelog.md",
	} {
		if err := gitStore.EnsureAbsent(context.Background(), "dogfood",
			legacy, "system",
			"Retire legacy .md seed (replaced by .html)"); err != nil {
			log.Printf("Warning: could not retire %s: %v", legacy, err)
		}
	}
	wtPath, err := gitStore.EnsureWorktree(context.Background(), "dogfood")
	if err != nil {
		log.Printf("Warning: could not materialize dogfood working tree: %v", err)
	} else {
		proj.ContentOverride = wtPath
		log.Printf("Git: dogfood worktree → %s", wtPath)
	}

	_ = gitStore.GCStaleProposals(context.Background(), 24*time.Hour)

	gitStore.OnInternalPush = func(ctx context.Context, workspace string) {
		_, _ = gitStore.AppendEvent(ctx, gitserver.Event{
			Workspace: workspace,
			Type:      "push",
			At:        time.Now().Unix(),
		})
	}

	gitHooks := gitserver.Hooks{
		OnPush: func(ctx context.Context, workspace string, refs []gitserver.PushedRef) {
			if _, err := gitStore.SyncWorktree(ctx, workspace); err != nil {
				log.Printf("Warning: working-tree sync after push to %s: %v", workspace, err)
				return
			}
			log.Printf("Git: pushed %d ref(s) to workspace %s", len(refs), workspace)
			_, _ = gitStore.AppendEvent(ctx, gitserver.Event{
				Workspace: workspace,
				Type:      "push",
				At:        time.Now().Unix(),
			})
		},
	}
	gitSrv, err := gitserver.New(gitStore, gitHooks)
	if err != nil {
		return fmt.Errorf("open git server: %w", err)
	}
	gitSrv.ProjectPath = proj.Path
	// Retrofit the pre-receive hook onto every existing bare repo at
	// boot so legacy workspaces fall under §5.2 enforcement on the
	// next push. Best-effort: warn but continue if a single hook
	// install fails — the server still gates writes at the HTTP
	// and MCP layers regardless.
	if err := gitStore.InstallAllHooks(); err != nil {
		log.Printf("Warning: install pre-receive hooks: %v", err)
	}

	// MCP shims that translate between the gitserver API and the
	// mcp-package types.
	proposeFn := func(ctx context.Context, req mcp.ProposeRequest) (*mcp.ProposeResult, error) {
		gsReq := gitserver.ProposeRequest{
			Workspace: req.Workspace,
			Base:      req.Base,
			Branch:    req.Branch,
			Message:   req.Message,
			Actor:     req.Actor,
		}
		for _, f := range req.Files {
			gsReq.Files = append(gsReq.Files, gitserver.ProposeFile{Path: f.Path, Body: f.Body})
		}
		r, err := gitStore.Propose(ctx, gsReq)
		if err != nil {
			return nil, err
		}
		return &mcp.ProposeResult{
			Success:    r.Success,
			Branch:     r.Branch,
			Commit:     r.Commit,
			ProposalID: r.ProposalID,
			Conflicts:  r.Conflicts,
			Message:    r.Message,
		}, nil
	}

	htmlSrv := &htmlserver.Server{
		WorktreeRoot: wtPath,
		Workspace:    "dogfood",
		Branch:       "main",
		UserResolver: func(r *http.Request) string {
			if u := auth.UserFromContext(r.Context()); u != nil {
				return u.Username
			}
			return ""
		},
		IsAdminFn: func(r *http.Request) bool {
			u := auth.UserFromContext(r.Context())
			return u != nil && u.Kind == auth.KindAdmin
		},
		HistoryFn: func(path string, limit int) ([]htmlserver.CommitInfo, error) {
			commits, err := gitStore.History(context.Background(), "dogfood", path, limit)
			if err != nil {
				return nil, err
			}
			out := make([]htmlserver.CommitInfo, 0, len(commits))
			for _, c := range commits {
				out = append(out, htmlserver.CommitInfo{
					SHA: c.SHA, Short: c.Short, Author: c.Author,
					When: c.When, Subject: c.Subject,
				})
			}
			return out, nil
		},
		DiffFn: func(path, from, to string) (string, error) {
			return gitStore.Diff(context.Background(), "dogfood", from, to, path)
		},
		SearchFn: func(q string, limit int) ([]htmlserver.SearchHit, error) {
			hits, err := searchStore.Query(context.Background(), "dogfood", q, limit)
			if err != nil {
				return nil, err
			}
			out := make([]htmlserver.SearchHit, 0, len(hits))
			for _, h := range hits {
				escaped := htmlEscape(h.Snippet)
				escaped = strings.ReplaceAll(escaped, htmlEscape(search.SnippetSentinelOpen), "<mark>")
				escaped = strings.ReplaceAll(escaped, htmlEscape(search.SnippetSentinelClose), "</mark>")
				out = append(out, htmlserver.SearchHit{
					Path:        h.Path,
					SnippetHTML: escaped,
				})
			}
			return out, nil
		},
	}

	// WriteCheck composes auth + groups + permissions package to gate
	// every write the runtime sees (handlers_edit, handlers_restore,
	// agentboard_propose). The git smart-HTTP push path is gated
	// separately via a pre-receive hook (follow-up).
	writeCheck := func(ctx context.Context, workspace, username string, paths []string) error {
		user, err := authStore.GetUser(username)
		if err != nil {
			return fmt.Errorf("permissions: lookup user %s: %w", username, err)
		}
		if user == nil {
			return fmt.Errorf("permissions: user %s not found", username)
		}
		grps, _ := groupStore.MemberOf(ctx, username)
		actor := permissions.Actor{
			Username: user.Username,
			Role:     permissions.Role(string(user.Kind)),
			Groups:   grps,
		}
		wtPath := gitStore.WorktreePath(workspace)
		rules, err := permissions.LoadForWorktree(wtPath)
		if err != nil {
			return fmt.Errorf("permissions: load rules: %w", err)
		}
		return rules.Allow(actor, paths)
	}

	srv := server.New(server.ServerConfig{
		Project:     proj,
		Conn:        dbConn.Conn(),
		Auth:        authStore,
		Invitations: invStore,
		Groups:      groupStore,
		SkillFile:   embedpkg.SkillFile(),
		GitServer:   gitSrv,
		HTML:        htmlSrv,
		PreviewFn:   server.PreviewFn(htmlSrv.RenderMarkdownPreview),
		WriteCheck:  server.WriteCheckFn(writeCheck),
		EditFn: func(ctx context.Context, workspace, path, body, actor, message string) error {
			return gitStore.PutFile(ctx, workspace, path, body, actor, message)
		},
		RestoreFn: func(ctx context.Context, workspace, path, sha, actor string) error {
			body, err := gitStore.FileAt(ctx, workspace, sha, path)
			if err != nil {
				return err
			}
			short := sha
			if len(short) > 10 {
				short = short[:10]
			}
			return gitStore.PutFile(ctx, workspace, path, body, actor,
				"Restore "+path+" to "+short)
		},
	})
	// SSE fan-out: any push (HTTP smart-protocol or server-internal)
	// now broadcasts a "workspace-changed" event over /_api/events so
	// open dashboard tabs can offer a non-modal "reload" toast.
	// Same hook also drives the FTS5 re-index.
	wireSSE := func(workspace string) {
		payload, _ := json.Marshal(map[string]string{"workspace": workspace})
		srv.Broadcaster.Broadcast(server.SSEEvent{
			Type: "workspace-changed",
			Data: payload,
		})
	}
	reindex := func(ctx context.Context, workspace string) {
		wt, err := gitStore.EnsureWorktree(ctx, workspace)
		if err != nil {
			log.Printf("Warning: search reindex worktree resolve: %v", err)
			return
		}
		if err := searchStore.ReindexWorktree(ctx, workspace, wt); err != nil {
			log.Printf("Warning: search reindex %s: %v", workspace, err)
		}
	}
	prevInternal := gitStore.OnInternalPush
	gitStore.OnInternalPush = func(ctx context.Context, workspace string) {
		if prevInternal != nil {
			prevInternal(ctx, workspace)
		}
		wireSSE(workspace)
		reindex(ctx, workspace)
	}
	prevHook := gitHooks.OnPush
	gitHooks.OnPush = func(ctx context.Context, workspace string, refs []gitserver.PushedRef) {
		if prevHook != nil {
			prevHook(ctx, workspace, refs)
		}
		wireSSE(workspace)
		reindex(ctx, workspace)
	}
	// Build the initial index synchronously so the dashboard's
	// search box works on first request after a fresh boot.
	reindex(context.Background(), "dogfood")

	srv.MCP.GitStore = gitStore
	srv.MCP.ProposeFn = proposeFn
	srv.MCP.WriteCheck = mcp.WriteCheckFn(writeCheck)
	srv.MCP.ResolveConflictFn = func(ctx context.Context, proposalID, file, resolution string) (*mcp.ProposeResult, error) {
		r, err := gitStore.ResolveConflict(ctx, proposalID, file, resolution)
		if err != nil {
			return nil, err
		}
		return &mcp.ProposeResult{
			Success:    r.Success,
			Branch:     r.Branch,
			Commit:     r.Commit,
			ProposalID: r.ProposalID,
			Conflicts:  r.Conflicts,
			Message:    r.Message,
		}, nil
	}
	addr := fmt.Sprintf(":%d", port)
	url := fmt.Sprintf("http://localhost:%d", port)

	fmt.Printf(`
AgentBoard %s

Project:   %s (%s)
Dashboard: %s
Git:       %s/git/dogfood.git
MCP:       %s/mcp

Connect Claude:
  claude mcp add agentboard %s/mcp

Press Ctrl+C to stop.

`, server.Version(), proj.Config.Title, projPath, url, url, url, url)

	if !noOpen {
		go func() {
			time.Sleep(500 * time.Millisecond)
			openBrowser(url)
		}()
	}

	log.Printf("Listening on %s", addr)
	return srv.ListenAndServe(addr)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}
