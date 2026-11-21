package org

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func loadClient(cmd *cobra.Command, loader providers.GrafanaConfigLoader) (*client, error) {
	deps, _, err := providers.LoadGrafanaDepsWithLoader(cmd.Context(), loader)
	if err != nil {
		return nil, err
	}
	return &client{deps: deps}, nil
}

func userTable() cmdio.Table[User] {
	return cmdio.Table[User]{Columns: []cmdio.Column[User]{
		{Header: "ID", Content: func(u User) string { return strconv.FormatInt(u.UserID, 10) }},
		{Header: "LOGIN", Content: func(u User) string { return u.Login }},
		{Header: "EMAIL", Content: func(u User) string { return u.Email }},
		{Header: "ROLE", Content: func(u User) string { return u.Role }},
		{Header: "NAME", Visible: cmdio.WideOnly, Content: func(u User) string { return u.Name }},
	}}
}

type readOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *readOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}
func (o *readOpts) Validate() error { return o.IO.Validate() }

type listResult struct {
	Users    []User          `json:"users" yaml:"users"`
	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

func (listResult) ListItemsKey() string { return "users" }

func newListCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	opts := &readOpts{}
	cmd := &cobra.Command{Use: "list", Short: "List users in the selected organization.", Args: cobra.NoArgs,
		Long: "List users in the selected organization. The API returns the full list. The limit trims the output after the fetch. Structured output contains users and, when truncated, list_meta.",
		Example: `  # List the first 50 users.
  gcx org users list

  # Include user names in the table.
  gcx org users list -o wide

  # Return every user as JSON.
  gcx org users list --limit 0 -o json`,
		Annotations: map[string]string{agent.AnnotationTokenCost: "small"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			c, err := loadClient(cmd, loader)
			if err != nil {
				return err
			}
			users, err := c.list(cmd.Context())
			if err != nil {
				return err
			}
			users, meta := cmdio.TruncateCompleteList(users, opts.Limit)
			meta = cmdio.AttachListMeta(meta, os.Args)
			if err := opts.IO.Encode(cmd.OutOrStdout(), listResult{Users: users, ListMeta: meta}); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
			return nil
		}}
	table := cmdio.TableAdapter[listResult, User]{Table: userTable(), Rows: func(result listResult) ([]User, error) { return result.Users, nil }}
	opts.IO.RegisterCustomCodec("table", table.Codec("table"))
	opts.IO.RegisterCustomCodec("wide", table.Codec("wide"))
	opts.setup(cmd.Flags())
	opts.IO.BindListLimit(cmd.Flags(), &opts.Limit, "users", 50)
	return cmd
}

func validateSubjects(args []string) error {
	for _, subject := range args {
		if strings.TrimSpace(subject) == "" {
			return errors.New("user identity must not be empty")
		}
	}
	return nil
}

func newGetCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	opts := &readOpts{}
	cmd := &cobra.Command{Use: "get <login|email|id>", Short: "Get a user in the selected organization.", Args: cobra.ExactArgs(1),
		Long: "Get one user by exact, case-sensitive login, email, or numeric ID. Lookup reads all users in the selected organization. Missing or ambiguous identities fail.",
		Example: `  # Get a user by login.
  gcx org users get alice

  # Get a user by email.
  gcx org users get alice@example.com

  # Get a user by ID with all table columns.
  gcx org users get 42 -o wide`,
		Annotations: map[string]string{agent.AnnotationTokenCost: "small"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := validateSubjects(args); err != nil {
				return err
			}
			c, err := loadClient(cmd, loader)
			if err != nil {
				return err
			}
			users, err := c.list(cmd.Context())
			if err != nil {
				return err
			}
			user, err := resolve(users, args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), user)
		}}
	table := cmdio.TableAdapter[User, User]{Table: userTable(), Rows: func(user User) ([]User, error) { return []User{user}, nil }}
	opts.IO.RegisterCustomCodec("table", table.Codec("table"))
	opts.IO.RegisterCustomCodec("wide", table.Codec("wide"))
	opts.setup(cmd.Flags())
	return cmd
}

type deleteOpts struct {
	IO     cmdio.Options
	Force  bool
	DryRun bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.BoolVar(&o.Force, "force", false, "Skip the confirmation prompt")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Resolve users and show the removal plan without a write request")
}
func (o *deleteOpts) Validate() error { return o.IO.Validate() }

// deleteResult adds targets only for a dry run. A count cannot identify the
// users who will lose access. Applied batches retain the shared count and
// failure shape and do not enumerate successful removals.
type deleteResult struct {
	cmdio.BatchMutation `yaml:",inline"`

	Targets []cmdio.MutationTarget `json:"targets,omitempty" yaml:"targets,omitempty"`
}

func newDeleteCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{Use: "remove <login|email|id>...", Short: "Remove users from the selected organization.", Args: cobra.MinimumNArgs(1),
		Long: "Remove organization membership only. Other organizations and the Grafana user account remain. Resolve each argument by exact, case-sensitive login, email, or numeric ID in the selected organization. Missing or ambiguous identities fail before any write. Duplicate users are removed once. A dry run shows the resolved users. A batch reports each failed removal.",
		Example: `  # Preview a removal without a write request.
  gcx org users remove alice@example.com --dry-run

  # Remove one membership after confirmation.
  gcx org users remove alice@example.com

  # Remove several memberships without a prompt.
  gcx org users remove alice@example.com bob@example.com --force`,
		Annotations: map[string]string{agent.AnnotationTokenCost: "small"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := validateSubjects(args); err != nil {
				return err
			}
			c, err := loadClient(cmd, loader)
			if err != nil {
				return err
			}
			users, err := c.list(cmd.Context())
			if err != nil {
				return err
			}
			targets, err := resolveTargets(users, args)
			if err != nil {
				return err
			}
			result := deleteResult{BatchMutation: cmdio.NewBatchMutation("removed")}
			result.DryRun = opts.DryRun
			if opts.DryRun {
				for _, user := range targets {
					result.Targets = append(result.Targets, mutationTarget(user))
				}
				result.Summary.Skipped = len(targets)
				result.SkippedReasons = map[string]int{"dry run": len(targets)}
				return opts.IO.Encode(cmd.OutOrStdout(), result)
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, fmt.Sprintf("Remove %d user(s) from the selected organization?", len(targets)))
			if err != nil {
				return err
			}
			if !proceed {
				exitCode := gcxerrors.ExitCancelled
				return &gcxerrors.DetailedError{Summary: "organization user removal cancelled", ExitCode: &exitCode}
			}
			result.BatchMutation = c.remove(cmd.Context(), targets)
			if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			if result.Summary.Failed > 0 {
				return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, errors.New("organization user removal failed"))
			}
			return nil
		}}
	opts.setup(cmd.Flags())
	return cmd
}

func resolveTargets(users []User, subjects []string) ([]User, error) {
	targets := []User{}
	seen := map[int64]bool{}
	for _, subject := range subjects {
		user, err := resolve(users, subject)
		if err != nil {
			return nil, err
		}
		if !seen[user.UserID] {
			targets = append(targets, user)
			seen[user.UserID] = true
		}
	}
	return targets, nil
}
func mutationTarget(user User) cmdio.MutationTarget {
	return cmdio.MutationTarget{Kind: "OrganizationUser", Name: user.Login, ID: strconv.FormatInt(user.UserID, 10), Namespace: strconv.FormatInt(user.OrgID, 10)}
}
