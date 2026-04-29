package cli

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/christophermarx/agentboard/internal/backup"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/spf13/cobra"
)

var (
	backupTo     string
	restoreFrom  string
	restoreForce bool
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Snapshot the project folder to a tarball (or future S3 path).",
	Long: `Produce a gzip'd tar of the project folder. Includes data, content,
files, components, .agentboard/ metadata, and the legacy SQLite KV.
Skips transient SQLite WAL/SHM files, .DS_Store, and the first-admin
invitation URL.

Phase 1 ships local-tarball only:

    agentboard backup --to ./snapshot.tar.gz
    agentboard backup --project agentboard-dev --to ./agentboard-dev-2026-04-28.tar.gz

S3 destinations (--to s3://bucket/path/) are reserved and will land
when AWS SDK gets bundled.

For absolute consistency, run with the server stopped — the same
caveat sqlite3's hot-backup applies. Atomic-rename means individual
files are always coherent; cross-key consistency is not guaranteed
during a live snapshot.`,
	RunE: runBackup,
}

func runBackup(cmd *cobra.Command, args []string) error {
	if backupTo == "" {
		return fmt.Errorf("--to is required")
	}
	if strings.HasPrefix(backupTo, "s3://") {
		return fmt.Errorf("S3 destinations are not yet supported (Phase 2 — AWS SDK pending). Use a local path for now.")
	}

	proj, err := project.Load(resolveProjectPath())
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}

	// Light health check — warn (don't fail) if the server is up against
	// this project. The user might be intentionally taking a hot backup.
	checkPort := proj.Config.Port
	if checkPort == 0 {
		checkPort = resolvePort()
	}
	if isServerLive(checkPort) {
		fmt.Fprintln(os.Stderr, "warning: a server appears to be running on this project; the backup may catch a mid-write moment.")
		fmt.Fprintln(os.Stderr, "         atomic-rename guarantees per-file consistency; cross-key consistency requires a stopped server.")
	}

	start := time.Now()
	files, bytes, err := backup.Backup(proj.Path, backupTo)
	if err != nil {
		return err
	}

	out, _ := os.Stat(backupTo)
	tarSize := int64(0)
	if out != nil {
		tarSize = out.Size()
	}

	fmt.Printf("Backup complete in %s\n", time.Since(start).Round(time.Millisecond))
	fmt.Printf("  Project:  %s\n", proj.Path)
	fmt.Printf("  Output:   %s (%s on disk)\n", backupTo, humanBytes(tarSize))
	fmt.Printf("  Files:    %d (%s uncompressed)\n", files, humanBytes(bytes))
	return nil
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Untar a backup archive into a project directory.",
	Long: `Restore a backup tarball produced by 'agentboard backup' into a target
project directory. By default refuses to write into a non-empty
directory; pass --force to override.

    agentboard restore --from ./snapshot.tar.gz --path ./fresh-project/
    agentboard restore --from ./snapshot.tar.gz --project recovered

Always run against a stopped server. The dashboard, MCP, and
in-memory indexes load on next start; until then the new files won't
be visible.`,
	RunE: runRestore,
}

func runRestore(cmd *cobra.Command, args []string) error {
	if restoreFrom == "" {
		return fmt.Errorf("--from is required")
	}

	target := projectPath
	if target == "" {
		target = resolveProjectPath()
	}

	// Soft guard: warn if a server is reachable on the resolved port.
	// Don't block — the user might be restoring into a fresh path that
	// happens to share a port with an unrelated running server. They
	// know their setup; we surface the risk and proceed.
	if isServerLive(resolvePort()) {
		fmt.Fprintln(os.Stderr, "warning: a server is reachable on the resolved port — if it serves THIS project, stop it first; otherwise ignore.")
	}

	count, err := backup.Restore(restoreFrom, target, restoreForce)
	if err != nil {
		return err
	}
	fmt.Printf("Restore complete: %d files written to %s\n", count, target)
	return nil
}

// isServerLive does a quick GET /api/health to see if there's an
// agentboard answering on the configured port. Best-effort — we use a
// 200 ms timeout so a missing server doesn't slow the CLI down.
func isServerLive(p int) bool {
	if p == 0 {
		p = 3000
	}
	client := &http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/health", p))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func init() {
	backupCmd.Flags().StringVar(&backupTo, "to", "", "Destination path for the tarball (.tar.gz). S3 URLs reserved for Phase 2.")
	restoreCmd.Flags().StringVar(&restoreFrom, "from", "", "Tarball to restore.")
	restoreCmd.Flags().BoolVar(&restoreForce, "force", false, "Overwrite contents of a non-empty target directory.")

	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(restoreCmd)
}
