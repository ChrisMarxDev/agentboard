package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
	dbpkg "github.com/christophermarx/agentboard/internal/db"
	"github.com/christophermarx/agentboard/internal/groups"
	"github.com/christophermarx/agentboard/internal/permissions"
	"github.com/spf13/cobra"
)

// Hidden subcommand invoked by the per-bare-repo pre-receive hook
// installed by gitserver.Store.InstallPreReceiveHook. The hook execs
// us with:
//
//   --workspace=<id>      workspace this push targets
//   --bare=<absolute>     bare repo path (= hook's $PWD)
//   --user=<username>     resolved auth user from the smart-HTTP layer
//   --project=<path>      project root (DB lives at .agentboard/data.sqlite)
//
// Stdin carries one line per ref update: "<old> <new> <ref>".
// Exit 0 → allow. Non-zero with a stderr message → deny; git relays
// the message to the client.

var (
	internalPreRecvWorkspace string
	internalPreRecvBare      string
	internalPreRecvUser      string
	internalPreRecvProject   string
)

var internalPreReceiveCmd = &cobra.Command{
	Use:    "internal-pre-receive",
	Short:  "Internal: pre-receive hook target. Not for direct use.",
	Hidden: true,
	RunE:   runInternalPreReceive,
}

func init() {
	internalPreReceiveCmd.Flags().StringVar(&internalPreRecvWorkspace, "workspace", "", "workspace id")
	internalPreReceiveCmd.Flags().StringVar(&internalPreRecvBare, "bare", "", "bare repo path")
	internalPreReceiveCmd.Flags().StringVar(&internalPreRecvUser, "user", "", "pushing user")
	internalPreReceiveCmd.Flags().StringVar(&internalPreRecvProject, "project", "", "project root path")
	rootCmd.AddCommand(internalPreReceiveCmd)
}

// zeroSHA is git's "no commit" sentinel used for branch create / delete.
const zeroSHA = "0000000000000000000000000000000000000000"

func runInternalPreReceive(cmd *cobra.Command, _ []string) error {
	if internalPreRecvUser == "" || internalPreRecvUser == "anonymous" {
		// Anonymous push shouldn't happen post-auth-middleware, but
		// if it does, deny.
		fmt.Fprintln(os.Stderr, "permission denied: anonymous push not allowed")
		os.Exit(1)
	}
	if internalPreRecvProject == "" {
		fmt.Fprintln(os.Stderr, "pre-receive hook misconfigured: AGENTBOARD_PROJECT_PATH unset")
		os.Exit(1)
	}
	if internalPreRecvBare == "" || internalPreRecvWorkspace == "" {
		fmt.Fprintln(os.Stderr, "pre-receive hook misconfigured: missing --bare or --workspace")
		os.Exit(1)
	}

	// Collect every changed path across every ref in this push.
	paths, err := collectChangedPaths(internalPreRecvBare, os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pre-receive: %v\n", err)
		os.Exit(1)
	}
	if len(paths) == 0 {
		// Pure ref-delete or no path-level change → allow.
		return nil
	}

	// Open project DB read-only-ish. We're a short-lived subprocess; the
	// parent agentboard server has the DB open too. SQLite WAL handles
	// concurrent readers fine.
	dbPath := filepath.Join(internalPreRecvProject, ".agentboard", "data.sqlite")
	conn, err := dbpkg.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pre-receive: open db: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	authStore, err := auth.NewStore(conn.Conn())
	if err != nil {
		fmt.Fprintf(os.Stderr, "pre-receive: open auth: %v\n", err)
		os.Exit(1)
	}
	groupStore, err := groups.NewStore(conn.Conn())
	if err != nil {
		fmt.Fprintf(os.Stderr, "pre-receive: open groups: %v\n", err)
		os.Exit(1)
	}

	user, err := authStore.GetUser(internalPreRecvUser)
	if err != nil || user == nil {
		fmt.Fprintf(os.Stderr, "pre-receive: unknown user %q\n", internalPreRecvUser)
		os.Exit(1)
	}
	ctx := context.Background()
	grps, _ := groupStore.MemberOf(ctx, user.Username)
	actor := permissions.Actor{
		Username: user.Username,
		Role:     permissions.Role(string(user.Kind)),
		Groups:   grps,
	}

	// Read permissions.yaml from the bare repo at HEAD. We evaluate
	// against the pre-push tree per the wiki-pivot design (structural
	// admin-only on .agentboard/ prevents the "non-admin grants self
	// admin" attack regardless of which tree we read).
	body, err := readBareBlob(internalPreRecvBare, "HEAD:"+permissions.PermissionsFilePath)
	rules := permissions.Rules{}
	if err == nil {
		rules, err = permissions.Parse(body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pre-receive: parse permissions.yaml: %v\n", err)
			os.Exit(1)
		}
	}
	// "Path doesn't exist at HEAD" → empty rules (open default), which
	// is the err != nil branch above.

	if err := rules.Allow(actor, paths); err != nil {
		var deny *permissions.DenyError
		if errors.As(err, &deny) {
			fmt.Fprintf(os.Stderr, "permission denied: %s\n", deny.Error())
		} else {
			fmt.Fprintf(os.Stderr, "permission check failed: %v\n", err)
		}
		os.Exit(1)
	}
	return nil
}

// collectChangedPaths reads "<old> <new> <ref>" lines from stdin and
// computes the union of changed paths across every ref. Special cases:
//
//   - old=zero, new=<sha>: branch create. Every file in NEW is "changed".
//   - new=zero: ref delete; no file-level change.
//   - otherwise: `git diff --name-only --no-renames OLD NEW`.
func collectChangedPaths(bare string, in io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(in)
	seen := map[string]struct{}{}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		oldSHA, newSHA := fields[0], fields[1]
		if newSHA == zeroSHA {
			continue // ref delete
		}
		var out []byte
		var err error
		if oldSHA == zeroSHA {
			out, err = gitInBare(bare, "ls-tree", "-r", "--name-only", newSHA)
		} else {
			out, err = gitInBare(bare, "diff", "--name-only", "--no-renames", oldSHA, newSHA)
		}
		if err != nil {
			return nil, fmt.Errorf("diff %s..%s: %w", oldSHA, newSHA, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			seen[line] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	return paths, nil
}

// gitInBare runs git with --git-dir set to the bare repo path.
func gitInBare(bare string, args ...string) ([]byte, error) {
	full := append([]string{"--git-dir=" + bare}, args...)
	return exec.Command("git", full...).Output()
}

// readBareBlob returns the bytes of a git object spec (e.g. "HEAD:path")
// from the bare repo. Returns an error when the object doesn't exist
// — caller should treat that as "absent" (permissive default).
func readBareBlob(bare, spec string) ([]byte, error) {
	return gitInBare(bare, "show", spec)
}
