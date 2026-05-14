package cli

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	dbpkg "github.com/christophermarx/agentboard/internal/db"
	embedpkg "github.com/christophermarx/agentboard/internal/embed"
	"github.com/christophermarx/agentboard/internal/gitserver"
	htmlserver "github.com/christophermarx/agentboard/internal/html"
	"github.com/christophermarx/agentboard/internal/invitations"
	"github.com/christophermarx/agentboard/internal/mcp"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/christophermarx/agentboard/internal/server"
	"github.com/spf13/cobra"
)

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
	if err := gitStore.EnsureFile(context.Background(), "dogfood",
		"skills/agentboard/SKILL.md", project.SeededSkillManifest, "system",
		"Add bootstrap SKILL"); err != nil {
		log.Printf("Warning: could not seed SKILL: %v", err)
	}
	if err := gitStore.EnsureFile(context.Background(), "dogfood",
		"skills/agentboard/examples.md", project.SeededSkillExamples, "system",
		"Add bootstrap examples"); err != nil {
		log.Printf("Warning: could not seed examples: %v", err)
	}
	// Demo content. HTML is the primary expressive primitive
	// (spec-filesystem-substrate.md); markdown stays for the
	// conventional surfaces (README, SKILL.md). Typed JSON for the
	// Taskboard. PutFile is content-idempotent: same body = no commit,
	// different body = a single update commit. The dogfood board is
	// the public showcase, so keeping the seeds current here is the
	// point. (Agents fork demo content into their own paths.)
	for _, seed := range []struct{ path, body, msg string }{
		{"index.html", project.SeededIndexHTML, "Update home page"},
		{"pages/getting-started.html", project.SeededGettingStartedHTML, "Update getting-started"},
		{"pages/changelog.html", project.SeededChangelogHTML, "Update changelog"},
		{"pages/roadmap.html", project.SeededRoadmapHTML, "Update roadmap"},
		{"taskboards/sprint.json", project.SeededSprintTaskboardJSON, "Update sprint board"},
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
	}

	srv := server.New(server.ServerConfig{
		Project:     proj,
		Conn:        dbConn.Conn(),
		Auth:        authStore,
		Invitations: invStore,
		SkillFile:   embedpkg.SkillFile(),
		GitServer:   gitSrv,
		HTML:        htmlSrv,
	})
	srv.MCP.GitStore = gitStore
	srv.MCP.ProposeFn = proposeFn
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
	srv.MCP.SubscribeFn = func(ctx context.Context, workspace string, since int64, types []string, limit int) (*mcp.SubscribeResult, error) {
		events, err := gitStore.ListEvents(ctx, workspace, since, types, limit)
		if err != nil {
			return nil, err
		}
		cursor := since
		out := make([]any, 0, len(events))
		for _, e := range events {
			out = append(out, e)
			if e.ID > cursor {
				cursor = e.ID
			}
		}
		if cursor == 0 {
			cursor, _ = gitStore.CurrentEventCursor(ctx)
		}
		return &mcp.SubscribeResult{Events: out, Cursor: cursor}, nil
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
