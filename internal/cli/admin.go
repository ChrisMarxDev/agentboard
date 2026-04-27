package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/data"
	"github.com/christophermarx/agentboard/internal/invitations"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/spf13/cobra"
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

	adminCmd.AddCommand(adminListCmd)
	adminCmd.AddCommand(adminListInvitationsCmd)
	adminCmd.AddCommand(adminRotateCmd)
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
	store, err := data.NewSQLiteStore(proj.DatabasePath())
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	authStore, err := auth.NewStore(store.DB())
	if err != nil {
		store.Close()
		return nil, nil, fmt.Errorf("open auth store: %w", err)
	}
	return authStore, func() { store.Close() }, nil
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
	store, err := data.NewSQLiteStore(proj.DatabasePath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()
	invStore, err := invitations.NewStore(store.DB())
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
