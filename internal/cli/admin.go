package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	dbpkg "github.com/christophermarx/agentboard/internal/db"
	"github.com/christophermarx/agentboard/internal/invitations"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Administer AgentBoard users + tokens (local file access; no network needed)",
	Long: `agentboard admin exposes local-file operations on the auth store:
list users, mint a fresh admin after a lockout, rotate a token, or rename
a user (the only path to change a username — usernames are immutable via
the web UI because they're the primary key that @mentions resolve to).

All commands require filesystem access to the project database. They do
NOT speak to any network API.`,
}

var adminListCmd = &cobra.Command{
	Use:   "list",
	Short: "List users and their tokens",
	RunE:  runAdminList,
}

var adminListInvitationsCmd = &cobra.Command{
	Use:   "list-invitations",
	Short: "List active invitation URLs (useful if the first-admin URL was lost)",
	RunE:  runAdminListInvitations,
}

var adminRotateCmd = &cobra.Command{
	Use:   "rotate <username> [token-label]",
	Short: "Rotate a user's token. If no label given and the user has one token, rotate it.",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runAdminRotate,
}

var (
	adminInviteRole      string
	adminInviteLabel     string
	adminInviteExpiresIn int
	adminInviteCreatedBy string
)

var adminInviteCmd = &cobra.Command{
	Use:   "invite",
	Short: "Mint a new invitation URL (no admin token needed; runs against the local DB)",
	Long: `Creates an invitation row directly via filesystem access to the
project database. Use this when no admin token is available — for
example, after a fresh deploy where the first-admin URL was lost, or
when an agent operating on the host needs to onboard a new user.

The printed URL is the same /invite/<id> link the admin UI emits.`,
	RunE: runAdminInvite,
}

var adminSetPasswordFromStdin bool

var adminSetPasswordCmd = &cobra.Command{
	Use:   "set-password <username>",
	Short: "Set or replace a user's browser password (lockout recovery)",
	Long: `Sets a password for the named user, enabling browser-session login
via /api/auth/login. The user must already exist (use "agentboard admin
invite" to onboard a new account first).

By default the password is read interactively; pass --from-stdin to
read it from STDIN for scripting. Plaintext is hashed with argon2id
before write — it is never persisted in cleartext or logged.`,
	Args: cobra.ExactArgs(1),
	RunE: runAdminSetPassword,
}

var adminRevokeSessionsCmd = &cobra.Command{
	Use:   "revoke-sessions <username>",
	Short: "Revoke every active browser session for a user",
	Long: `Marks every unrevoked row in user_sessions for the named user as
revoked. Bearer tokens (PATs, OAuth access tokens) are NOT touched —
use 'agentboard admin rotate' for those. This is the lockout-recovery
hammer for "I think my browser cookie was stolen".`,
	Args: cobra.ExactArgs(1),
	RunE: runAdminRevokeSessions,
}

var adminRenameUserYes bool

var adminRenameUserCmd = &cobra.Command{
	Use:   "rename-user <old> <new>",
	Short: "Rename a user (escape hatch — usernames are normally immutable)",
	Long: `Renames a user AND every token they own. Other string references
(data.updated_by, @mentions in MDX page content, assignees arrays on
cards, etc.) are NOT rewritten — mentions of the old username in free
text will silently stop resolving.

Usernames are designed to be immutable so @mentions and attribution
strings keep meaning across time; this command exists only for fixing
typos shortly after a user was created. Confirm with --yes to skip the
prompt.`,
	Args: cobra.ExactArgs(2),
	RunE: runAdminRenameUser,
}

func init() {
	adminRenameUserCmd.Flags().BoolVar(&adminRenameUserYes, "yes", false, "Skip the confirmation prompt")

	adminInviteCmd.Flags().StringVar(&adminInviteRole, "role", "member", "Role for the invitee: admin, member, or bot")
	adminInviteCmd.Flags().StringVar(&adminInviteLabel, "label", "", "Optional human label stored on the invitation")
	adminInviteCmd.Flags().IntVar(&adminInviteExpiresIn, "expires-in-days", 7, "Days until the invitation expires")
	adminInviteCmd.Flags().StringVar(&adminInviteCreatedBy, "created-by", "cli", "Audit string written to created_by (defaults to \"cli\")")

	adminSetPasswordCmd.Flags().BoolVar(&adminSetPasswordFromStdin, "from-stdin", false,
		"Read the new password from STDIN (no prompt, no echo). Useful in scripts.")

	adminCmd.AddCommand(adminListCmd)
	adminCmd.AddCommand(adminListInvitationsCmd)
	adminCmd.AddCommand(adminRotateCmd)
	adminCmd.AddCommand(adminInviteCmd)
	adminCmd.AddCommand(adminSetPasswordCmd)
	adminCmd.AddCommand(adminRevokeSessionsCmd)
	adminCmd.AddCommand(adminRenameUserCmd)
	rootCmd.AddCommand(adminCmd)
}

func openAuthStore() (*auth.Store, func(), error) {
	projPath := resolveProjectPath()
	if _, err := os.Stat(projPath); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("project not found at %s — run `agentboard serve` once to create it", projPath)
	}
	proj, err := project.Load(projPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load project: %w", err)
	}
	dbConn, err := dbpkg.Open(proj.DatabasePath())
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	authStore, err := auth.NewStore(dbConn.Conn())
	if err != nil {
		dbConn.Close()
		return nil, nil, fmt.Errorf("open auth store: %w", err)
	}
	return authStore, func() { dbConn.Close() }, nil
}

func runAdminList(cmd *cobra.Command, _ []string) error {
	a, closer, err := openAuthStore()
	if err != nil {
		return err
	}
	defer closer()

	users, err := a.ListUsers(true)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "USERNAME\tDISPLAY\tKIND\tMODE\tTOKENS\tLAST USED\tDEACTIVATED\tCREATED")
	for _, u := range users {
		activeTokens := 0
		var lastUsed time.Time
		for _, t := range u.Tokens {
			if t.RevokedAt == nil {
				activeTokens++
			}
			if t.LastUsedAt != nil && t.LastUsedAt.After(lastUsed) {
				lastUsed = *t.LastUsedAt
			}
		}
		lastUsedStr := "-"
		if !lastUsed.IsZero() {
			lastUsedStr = humanDuration(time.Since(lastUsed)) + " ago"
		}
		deactivated := "-"
		if u.DeactivatedAt != nil {
			deactivated = u.DeactivatedAt.Local().Format("2006-01-02")
		}
		display := u.DisplayName
		if display == "" {
			display = "-"
		}
		fmt.Fprintf(tw, "@%s\t%s\t%s\t%s\t%d/%d\t%s\t%s\t%s\n",
			u.Username, display, u.Kind, u.AccessMode,
			activeTokens, len(u.Tokens),
			lastUsedStr, deactivated,
			u.CreatedAt.Local().Format("2006-01-02"))
	}
	return tw.Flush()
}

// runAdminListInvitations prints every active invitation with its
// redeem URL. Primary use-case: the operator lost the first-admin URL
// that `serve` printed at boot. Also useful for auditing who's been
// invited and when the codes expire.
func runAdminListInvitations(cmd *cobra.Command, _ []string) error {
	projPath := resolveProjectPath()
	proj, err := project.Load(projPath)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	dbConn, err := dbpkg.Open(proj.DatabasePath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer dbConn.Close()
	invStore, err := invitations.NewStore(dbConn.Conn())
	if err != nil {
		return fmt.Errorf("open invitations store: %w", err)
	}
	list, err := invStore.List(false)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("No active invitations. Mint a new one via the admin UI, or restart `serve` on an empty DB to get a first-admin invite.")
		return nil
	}
	port := proj.Config.Port
	if port == 0 {
		port = 3000
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tROLE\tCREATED BY\tEXPIRES\tLABEL\tREDEEM URL")
	for _, inv := range list {
		label := inv.Label
		if label == "" {
			label = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t@%s\t%s\t%s\thttp://localhost:%d/invite/%s\n",
			inv.ID, inv.Role, inv.CreatedBy,
			humanDuration(time.Until(inv.ExpiresAt)),
			label, port, inv.ID)
	}
	return tw.Flush()
}

func runAdminInvite(cmd *cobra.Command, _ []string) error {
	role := invitations.Role(strings.ToLower(adminInviteRole))
	if !invitations.ValidRole(role) {
		return fmt.Errorf("--role must be admin, member, or bot (got %q)", adminInviteRole)
	}
	createdBy := strings.TrimSpace(adminInviteCreatedBy)
	if createdBy == "" {
		return fmt.Errorf("--created-by must not be empty")
	}
	if createdBy == invitations.BootstrapCreator {
		return fmt.Errorf("--created-by=%q is reserved for the first-admin bootstrap flow", invitations.BootstrapCreator)
	}
	if adminInviteExpiresIn <= 0 {
		return fmt.Errorf("--expires-in-days must be > 0")
	}

	projPath := resolveProjectPath()
	proj, err := project.Load(projPath)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	dbConn, err := dbpkg.Open(proj.DatabasePath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer dbConn.Close()
	invStore, err := invitations.NewStore(dbConn.Conn())
	if err != nil {
		return fmt.Errorf("open invitations store: %w", err)
	}

	inv, err := invStore.Create(invitations.CreateParams{
		Role:      role,
		CreatedBy: createdBy,
		ExpiresIn: time.Duration(adminInviteExpiresIn) * 24 * time.Hour,
		Label:     adminInviteLabel,
	})
	if err != nil {
		return fmt.Errorf("create invitation: %w", err)
	}

	port := proj.Config.Port
	if port == 0 {
		port = 3000
	}
	fmt.Printf(`Created %s invitation %s (expires in %dd, created_by=%s).

  http://localhost:%d/invite/%s

Replace localhost with your public hostname if the server is reachable elsewhere.
`, inv.Role, inv.ID, adminInviteExpiresIn, createdBy, port, inv.ID)
	return nil
}

func runAdminRotate(cmd *cobra.Command, args []string) error {
	a, closer, err := openAuthStore()
	if err != nil {
		return err
	}
	defer closer()

	user, err := a.GetUser(args[0])
	if err != nil {
		return fmt.Errorf("user @%s not found", args[0])
	}
	tokens, err := a.ListTokensForUser(user.Username)
	if err != nil {
		return err
	}

	var target *auth.UserToken
	if len(args) == 2 {
		for i := range tokens {
			if tokens[i].Label == args[1] && tokens[i].RevokedAt == nil {
				target = &tokens[i]
				break
			}
		}
		if target == nil {
			return fmt.Errorf("no active token with label %q on @%s", args[1], user.Username)
		}
	} else {
		for i := range tokens {
			if tokens[i].RevokedAt == nil {
				if target != nil {
					return fmt.Errorf("@%s has multiple active tokens; pass the label as second arg", user.Username)
				}
				target = &tokens[i]
			}
		}
		if target == nil {
			return fmt.Errorf("@%s has no active tokens; issue an invitation from /admin or create a token in the UI", user.Username)
		}
	}

	newToken, err := auth.GenerateToken()
	if err != nil {
		return err
	}
	if err := a.RotateToken(target.ID, auth.HashToken(newToken)); err != nil {
		return fmt.Errorf("rotate: %w", err)
	}
	fmt.Printf(`Rotated token %q on @%s. Paste the new value:

  %s

The previous token stops working immediately.
`, target.Label, user.Username, newToken)
	return nil
}

func runAdminRenameUser(cmd *cobra.Command, args []string) error {
	a, closer, err := openAuthStore()
	if err != nil {
		return err
	}
	defer closer()

	oldName, newName := strings.ToLower(args[0]), strings.ToLower(args[1])

	if !adminRenameUserYes {
		fmt.Fprintf(os.Stderr, `WARNING: renaming @%s → @%s.

This updates the user row and every token they own. It does NOT rewrite
free-text references in MDX pages, data values, data_history, or
assignees arrays on cards. Mentions of @%s elsewhere will silently stop
resolving.

Type the new username (@%s) to confirm: `, oldName, newName, oldName, newName)
		var got string
		fmt.Scanln(&got)
		if strings.TrimSpace(got) != newName {
			return fmt.Errorf("aborted")
		}
	}

	stats, err := a.RenameUser(oldName, newName)
	if err != nil {
		return err
	}
	fmt.Printf("Renamed @%s → @%s. Rows updated: users=%d, user_tokens=%d.\n",
		oldName, newName, stats.UsersUpdated, stats.TokensUpdated)
	return nil
}

func runAdminSetPassword(cmd *cobra.Command, args []string) error {
	a, closer, err := openAuthStore()
	if err != nil {
		return err
	}
	defer closer()

	username := strings.ToLower(strings.TrimSpace(args[0]))
	user, err := a.GetUser(username)
	if err != nil {
		return fmt.Errorf("user @%s not found", username)
	}

	password, err := readPassword(adminSetPasswordFromStdin)
	if err != nil {
		return err
	}
	if err := a.SetPassword(user.Username, password); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Password updated for @%s. Browser login at /login is now available.\n",
		user.Username)
	return nil
}

func runAdminRevokeSessions(cmd *cobra.Command, args []string) error {
	a, closer, err := openAuthStore()
	if err != nil {
		return err
	}
	defer closer()

	username := strings.ToLower(strings.TrimSpace(args[0]))
	if _, err := a.GetUser(username); err != nil {
		return fmt.Errorf("user @%s not found", username)
	}
	n, err := a.RevokeAllSessionsForUser(username)
	if err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Revoked %d session(s) for @%s. Bearer tokens were NOT touched.\n", n, username)
	return nil
}

// readPassword pulls a password either from STDIN (when --from-stdin
// is set or the input is piped) or from an interactive prompt that
// disables terminal echo. The interactive path requires a confirm-
// retype to catch typos; --from-stdin trusts the caller.
func readPassword(fromStdin bool) (string, error) {
	if fromStdin {
		buf, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		// Strip a trailing newline if the caller piped `echo "..."`.
		s := strings.TrimRight(string(buf), "\r\n")
		if len(s) < auth.MinPasswordLen {
			return "", auth.ErrWeakPassword
		}
		return s, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("not a terminal; pass --from-stdin to feed the password as a pipe")
	}
	fmt.Fprint(os.Stderr, "New password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if len(first) < auth.MinPasswordLen {
		return "", auth.ErrWeakPassword
	}
	fmt.Fprint(os.Stderr, "Retype to confirm: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read confirmation: %w", err)
	}
	if string(first) != string(second) {
		return "", fmt.Errorf("passwords did not match — try again")
	}
	return string(first), nil
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
