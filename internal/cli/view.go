package cli

// `agentboard view <path>` boots a read-only renderer over a local
// directory and opens the browser at the bound port. It's the
// desktop-style viewer: same HTML the hosted dashboard serves, but
// pointed at a folder on disk with no auth, no MCP, no git smart-HTTP,
// no /_api surface. The directory may be a git working tree (history
// + diff views light up) or a plain folder (those views report "not
// available"). Either way, every write path is structurally inert —
// the renderer is constructed with ReadOnly: true and the router
// mounts nothing else.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	htmlserver "github.com/christophermarx/agentboard/internal/html"
	"github.com/spf13/cobra"
)

var viewCmd = &cobra.Command{
	Use:   "view [path]",
	Short: "Open a local directory in a read-only AgentBoard dashboard",
	Long: `Render any local directory (or git working tree) through the
AgentBoard dashboard, served on an ephemeral localhost port. Read-only:
no sign-in, no edit, no /_api. If the directory is a git repo, the
?history=1 and ?diff=... views light up against your local git.

Examples:
  agentboard view              # current directory
  agentboard view ~/some/repo
  agentboard view ~/notes --port 4000`,
	Args: cobra.MaximumNArgs(1),
	RunE: runView,
}

func init() {
	viewCmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't open browser on startup")
	rootCmd.AddCommand(viewCmd)
}

func runView(cmd *cobra.Command, args []string) error {
	target := "."
	if len(args) == 1 {
		target = args[0]
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", abs)
	}

	// Detect git repo. If the directory (or any parent that contains
	// our path as its worktree) is a git repo, we wire history+diff;
	// otherwise those views fall back to "not available".
	repoRoot, branch := detectGit(abs)

	htmlSrv := &htmlserver.Server{
		WorktreeRoot: abs,
		Workspace:    filepath.Base(abs),
		Branch:       branch,
		ReadOnly:     true,
	}
	if repoRoot != "" {
		htmlSrv.HistoryFn = func(p string, limit int) ([]htmlserver.CommitInfo, error) {
			return gitHistory(repoRoot, p, limit)
		}
		htmlSrv.DiffFn = func(p, from, to string) (string, error) {
			return gitDiff(repoRoot, p, from, to)
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/", htmlSrv)

	addr := "127.0.0.1:0"
	if port > 0 {
		addr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	url := fmt.Sprintf("http://%s", ln.Addr().String())

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Printf(`
AgentBoard viewer (read-only)

Directory: %s
URL:       %s
`, abs, url)
	if repoRoot != "" {
		fmt.Printf("Git:       %s (branch %s)\n", repoRoot, branch)
	}
	fmt.Println("\nPress Ctrl+C to stop.")

	if !noOpen {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(url)
		}()
	}

	log.Printf("Listening on %s", ln.Addr())
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// detectGit returns the repo root (the dir containing .git) and the
// current branch name, or ("", "") if `dir` isn't inside a git repo.
// We delegate to git so worktrees, submodules, and detached HEADs all
// behave the way the user expects.
func detectGit(dir string) (root, branch string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", ""
	}
	root = strings.TrimSpace(string(out))
	b, err := exec.CommandContext(ctx, "git", "-C", dir, "branch", "--show-current").Output()
	if err == nil {
		branch = strings.TrimSpace(string(b))
	}
	if branch == "" {
		branch = "HEAD"
	}
	return root, branch
}

// gitHistory returns commits that touched `urlPath` (relative to the
// repo root), newest first. urlPath comes from the URL ("/foo/bar.md");
// we strip the leading slash to make it a repo-relative path. The
// renderer always passes a non-empty path (`?history=1` isn't shown
// on the index).
func gitHistory(repoRoot, urlPath string, limit int) ([]htmlserver.CommitInfo, error) {
	rel := strings.TrimPrefix(urlPath, "/")
	if rel == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Format: SHA\x1fauthor\x1fISO date\x1fsubject — record-separator
	// chars keep us safe against authors with tabs in their names.
	const sep = "\x1f"
	format := "%H" + sep + "%an" + sep + "%aI" + sep + "%s"
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"log", fmt.Sprintf("-%d", limit), "--pretty="+format, "--", rel).Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	commits := make([]htmlserver.CommitInfo, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, sep, 4)
		if len(parts) < 4 {
			continue
		}
		short := parts[0]
		if len(short) > 10 {
			short = short[:10]
		}
		commits = append(commits, htmlserver.CommitInfo{
			SHA:     parts[0],
			Short:   short,
			Author:  parts[1],
			When:    parts[2],
			Subject: parts[3],
		})
	}
	return commits, nil
}

// gitDiff returns the textual diff of `urlPath` between revisions
// `from` and `to`. Empty `from` means "the parent of to" — same
// convention as the hosted gitserver.Diff.
func gitDiff(repoRoot, urlPath, from, to string) (string, error) {
	rel := strings.TrimPrefix(urlPath, "/")
	if rel == "" {
		return "", fmt.Errorf("diff requires a file path")
	}
	if to == "" {
		return "", fmt.Errorf("diff requires a target revision")
	}
	if from == "" {
		from = to + "^"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"diff", from+".."+to, "--", rel).Output()
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return string(out), nil
}
