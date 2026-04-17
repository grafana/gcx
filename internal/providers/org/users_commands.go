package org

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

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

// usersCommands returns the users command group.
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

// --- list ---

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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			users, err := client.ListUsers(ctx)
			if err != nil {
				return err
			}

			users = adapter.TruncateSlice(users, opts.Limit)
			return opts.IO.Encode(cmd.OutOrStdout(), users)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// UsersTableCodec renders org users as a table.
type UsersTableCodec struct{}

func (c *UsersTableCodec) Format() format.Format { return "table" }

func (c *UsersTableCodec) Encode(w io.Writer, v any) error {
	users, ok := v.([]OrgUser)
	if !ok {
		return errors.New("invalid data type for table codec: expected []OrgUser")
	}

	t := style.NewTable("USER_ID", "LOGIN", "NAME", "EMAIL", "ROLE", "LAST_SEEN")
	for _, u := range users {
		lastSeen := u.LastSeenAtAge
		if lastSeen == "" {
			lastSeen = u.LastSeenAt
		}
		t.Row(strconv.Itoa(u.UserID), u.Login, u.Name, u.Email, u.Role, lastSeen)
	}
	return t.Render(w)
}

func (c *UsersTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
}

// --- get ---

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
			needle := strings.ToLower(strings.TrimSpace(args[0]))

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			users, err := client.ListUsers(ctx)
			if err != nil {
				return err
			}

			for i := range users {
				if strings.EqualFold(users[i].Login, needle) || strings.EqualFold(users[i].Email, needle) {
					// Encode as a single-item slice so the table codec can render it.
					return opts.IO.Encode(cmd.OutOrStdout(), []OrgUser{users[i]})
				}
			}
			return fmt.Errorf("user %q: %w", args[0], ErrNotFound)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- add ---

type usersAddOpts struct {
	Login string
	Role  string
}

func newUsersAddCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &usersAddOpts{}
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a user to the current organization.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Login == "" {
				return errors.New("--login is required")
			}
			if opts.Role == "" {
				return errors.New("--role is required")
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			if err := client.AddUser(ctx, AddUserRequest{
				LoginOrEmail: opts.Login,
				Role:         opts.Role,
			}); err != nil {
				return err
			}

			cmdio.Info(cmd.OutOrStdout(), "Added user %s to organization with role %s.", opts.Login, opts.Role)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Login, "login", "", "Login or email of the user to add (required)")
	cmd.Flags().StringVar(&opts.Role, "role", "", "Role for the user, e.g. Admin, Editor, Viewer (required)")
	return cmd
}

// --- update-role ---

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
			userID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid user id %q: %w", args[0], err)
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			if err := client.UpdateUserRole(ctx, userID, opts.Role); err != nil {
				return err
			}

			cmdio.Info(cmd.OutOrStdout(), "Updated user %d role to %s.", userID, opts.Role)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Role, "role", "", "New role for the user, e.g. Admin, Editor, Viewer (required)")
	return cmd
}

// --- remove ---

func newUsersRemoveCommand(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove USER-ID",
		Short: "Remove a user from the current organization.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid user id %q: %w", args[0], err)
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			if err := client.RemoveUser(ctx, userID); err != nil {
				return err
			}

			cmdio.Info(cmd.OutOrStdout(), "Removed user %d from organization.", userID)
			return nil
		},
	}
	return cmd
}
