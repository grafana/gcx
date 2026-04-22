package org

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GrafanaConfigLoader can load a NamespacedRESTConfig from the active context.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

func usersCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage users in the current organization.",
	}
	cmd.AddCommand(
		newUsersListCommand(loader),
		newUsersGetCommand(loader),
		newUsersAddCommand(loader),
		newUsersUpdateRoleCommand(loader),
		newUsersRemoveCommand(loader),
	)
	return cmd
}

// parseUserID parses a positional USER-ID argument.
func parseUserID(arg string) (int, error) {
	id, err := strconv.Atoi(arg)
	if err != nil {
		return 0, fmt.Errorf("invalid user id %q: %w", arg, err)
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// list command
// ---------------------------------------------------------------------------

type usersListOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *usersListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &UsersTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for unlimited)")
}

func newUsersListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &usersListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List users in the current organization.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewUsersTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, opts.Limit)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), typedObjs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// UsersTableCodec renders org users as a table.
type UsersTableCodec struct{}

// Format returns the output format name.
func (c *UsersTableCodec) Format() format.Format { return "table" }

// Encode writes users to the writer as a table.
// It accepts []adapter.TypedObject[OrgUser] (from commands) and extracts .Spec internally.
func (c *UsersTableCodec) Encode(w io.Writer, v any) error {
	typedObjs, ok := v.([]adapter.TypedObject[OrgUser])
	if !ok {
		return errors.New("invalid data type for table codec: expected []TypedObject[OrgUser]")
	}

	t := style.NewTable("USER_ID", "LOGIN", "NAME", "EMAIL", "ROLE", "LAST_SEEN")
	for _, obj := range typedObjs {
		u := obj.Spec
		lastSeen := u.LastSeenAtAge
		if lastSeen == "" {
			lastSeen = u.LastSeenAt
		}
		t.Row(strconv.Itoa(u.UserID), u.Login, u.Name, u.Email, u.Role, lastSeen)
	}
	return t.Render(w)
}

// Decode is not supported for table format.
func (c *UsersTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// get command
// ---------------------------------------------------------------------------

type usersGetOpts struct {
	IO cmdio.Options
}

func (o *usersGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &UsersTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newUsersGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &usersGetOpts{}
	cmd := &cobra.Command{
		Use:   "get LOGIN-OR-EMAIL",
		Short: "Get a single org user by login or email.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, cfg, err := NewUsersTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			// Resolve login/email → numeric ID using a raw client call,
			// then route through TypedCRUD.Get by the canonical ID name.
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			user, err := client.GetByLoginOrEmail(ctx, args[0])
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, strconv.Itoa(user.UserID))
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), []adapter.TypedObject[OrgUser]{*typedObj})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// add command
// ---------------------------------------------------------------------------

type usersAddOpts struct {
	Login string
	Role  string
}

func (o *usersAddOpts) Validate() error {
	if o.Login == "" {
		return errors.New("--login is required")
	}
	if o.Role == "" {
		return errors.New("--role is required")
	}
	return nil
}

func newUsersAddCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &usersAddOpts{}
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a user to the current organization.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, cfg, err := NewUsersTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			ou := OrgUser{
				LoginOrEmail: opts.Login,
				Role:         opts.Role,
			}
			typedObj := &adapter.TypedObject[OrgUser]{Spec: ou}
			typedObj.SetNamespace(cfg.Namespace)

			created, err := crud.Create(ctx, typedObj)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Added user %s to organization with role %s (id=%d).",
				opts.Login, opts.Role, created.Spec.UserID)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Login, "login", "", "Login or email of the user to add (required)")
	cmd.Flags().StringVar(&opts.Role, "role", "", "Role for the user, e.g. Admin, Editor, Viewer (required)")
	return cmd
}

// ---------------------------------------------------------------------------
// update-role command
// ---------------------------------------------------------------------------

type usersUpdateRoleOpts struct {
	Role string
}

func newUsersUpdateRoleCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &usersUpdateRoleOpts{}
	cmd := &cobra.Command{
		Use:   "update-role USER-ID",
		Short: "Update the role of a user in the current organization.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Role == "" {
				return errors.New("--role is required")
			}
			userID, err := parseUserID(args[0])
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, cfg, err := NewUsersTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			ou := OrgUser{Role: opts.Role}
			typedObj := &adapter.TypedObject[OrgUser]{Spec: ou}
			typedObj.SetNamespace(cfg.Namespace)

			if _, err := crud.Update(ctx, args[0], typedObj); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated user %d role to %s.", userID, opts.Role)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Role, "role", "", "New role for the user, e.g. Admin, Editor, Viewer (required)")
	return cmd
}

// ---------------------------------------------------------------------------
// remove command
// ---------------------------------------------------------------------------

func newUsersRemoveCommand(loader GrafanaConfigLoader) *cobra.Command {
	return &cobra.Command{
		Use:   "remove USER-ID",
		Short: "Remove a user from the current organization.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userID, err := parseUserID(args[0])
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewUsersTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			if err := crud.Delete(ctx, args[0]); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Removed user %d from organization.", userID)
			return nil
		},
	}
}
