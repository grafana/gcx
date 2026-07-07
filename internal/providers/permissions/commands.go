package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/coreapi"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GrafanaConfigLoader can load a NamespacedRESTConfig from the active context.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

func clientFromLoader(ctx context.Context, loader GrafanaConfigLoader) (*Client, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, err
	}
	return NewClient(cfg)
}

const resourceArgHelp = "<resource> is one of: folders, dashboards, datasources, teams, serviceaccounts"

// ---- get ----

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &PermissionsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:     "get <resource> <id>",
		Short:   "Get the permission list for a resource instance.",
		Long:    "Get the granular RBAC permission list for a resource instance.\n\n" + resourceArgHelp,
		Example: "  gcx permissions get dashboards my-dashboard-uid\n  gcx permissions get folders my-folder-uid -o json",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateResource(args[0]); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := clientFromLoader(ctx, loader)
			if err != nil {
				return err
			}
			perms, err := client.Get(ctx, args[0], args[1])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), perms)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---- set ----

type setOpts struct {
	File string
}

func newSetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &setOpts{}
	cmd := &cobra.Command{
		Use:   "set <resource> <id> -f FILE",
		Short: "Replace the full permission set for a resource instance.",
		Long: "Replace the full permission set for a resource instance from a JSON file.\n\n" +
			"The file is a JSON array of assignments (or a {\"permissions\": [...]} object),\n" +
			"each with a \"permission\" level (View/Edit/Admin) and exactly one of\n" +
			"\"userId\", \"teamId\", or \"builtInRole\".\n\n" + resourceArgHelp,
		Example: "  gcx permissions set dashboards my-uid -f acl.json\n  cat acl.json | gcx permissions set folders my-uid -f -",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateResource(args[0]); err != nil {
				return err
			}
			perms, err := readPermissionsFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := clientFromLoader(ctx, loader)
			if err != nil {
				return err
			}
			if err := client.Set(ctx, args[0], args[1], perms); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "updated permissions for %s %s", args[0], args[1])
			return nil
		},
	}
	cmd.Flags().StringVarP(&opts.File, "file", "f", "", "JSON file with the permission set (use - for stdin)")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// ---- grant ----

type grantOpts struct {
	User  string
	Team  string
	Role  string
	Level string
}

func (o *grantOpts) validate() (string, string, error) {
	var principalKind, principalRef string
	set := 0
	if o.User != "" {
		set++
		principalKind, principalRef = "user", o.User
	}
	if o.Team != "" {
		set++
		principalKind, principalRef = "team", o.Team
	}
	if o.Role != "" {
		set++
		principalKind, principalRef = "role", o.Role
	}
	if set != 1 {
		return "", "", errors.New("provide exactly one of --user, --team, or --role")
	}
	if o.Level == "" {
		return "", "", errors.New("--level is required (e.g. View, Edit, Admin; use \"\" via set to remove)")
	}
	return principalKind, principalRef, nil
}

func newGrantCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &grantOpts{}
	cmd := &cobra.Command{
		Use:   "grant <resource> <id>",
		Short: "Grant a permission level to a single user, team, or built-in role.",
		Long: "Grant a permission level to a single principal on a resource instance.\n\n" +
			"Specify exactly one of --user, --team, or --role, plus --level.\n\n" + resourceArgHelp,
		Example: "  gcx permissions grant dashboards my-uid --team 3 --level Edit\n" +
			"  gcx permissions grant folders my-uid --role Viewer --level View\n" +
			"  gcx permissions grant datasources my-uid --user alice --level Admin",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateResource(args[0]); err != nil {
				return err
			}
			principalKind, principalRef, err := opts.validate()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := clientFromLoader(ctx, loader)
			if err != nil {
				return err
			}

			resource, id := args[0], args[1]
			switch principalKind {
			case "user":
				err = client.SetUserPermission(ctx, resource, id, principalRef, opts.Level)
			case "team":
				err = client.SetTeamPermission(ctx, resource, id, principalRef, opts.Level)
			case "role":
				err = client.SetBuiltInRolePermission(ctx, resource, id, principalRef, opts.Level)
			}
			if err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "granted %s permission to %s %s on %s %s", opts.Level, principalKind, principalRef, resource, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.User, "user", "", "User to grant to (numeric ID or UID)")
	cmd.Flags().StringVar(&opts.Team, "team", "", "Team to grant to (numeric ID or UID)")
	cmd.Flags().StringVar(&opts.Role, "role", "", "Built-in role to grant to (e.g. Viewer, Editor, Admin)")
	cmd.Flags().StringVar(&opts.Level, "level", "", "Permission level (e.g. View, Edit, Admin)")
	return cmd
}

// ---- levels ----

type levelsOpts struct {
	IO cmdio.Options
}

func (o *levelsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &DescriptionTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newLevelsCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &levelsOpts{}
	cmd := &cobra.Command{
		Use:     "levels <resource>",
		Short:   "Show the assignable permission levels for a resource kind.",
		Long:    "Show the assignable permission levels and assignment types for a resource kind.\n\n" + resourceArgHelp,
		Example: "  gcx permissions levels dashboards",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateResource(args[0]); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := clientFromLoader(ctx, loader)
			if err != nil {
				return err
			}
			desc, err := client.Describe(ctx, args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), desc)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---- input parsing ----

// readPermissionsFromFile reads a JSON permission set from path (or stdin for
// "-"). The input may be a bare array of assignments or a {"permissions":[...]}
// envelope.
func readPermissionsFromFile(path string, stdin io.Reader) ([]SetResourcePermissionCommand, error) {
	if path == "" {
		return nil, errors.New("--file is required")
	}
	data, err := coreapi.ReadInput(path, stdin)
	if err != nil {
		return nil, err
	}
	return parsePermissions(data)
}

// parsePermissions accepts either a bare JSON array or a {"permissions":[…]} envelope.
func parsePermissions(data []byte) ([]SetResourcePermissionCommand, error) {
	var perms []SetResourcePermissionCommand
	if err := json.Unmarshal(data, &perms); err == nil {
		return perms, nil
	}
	var envelope setPermissionsBody
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse permissions: expected JSON array or object with 'permissions' field: %w", err)
	}
	return envelope.Permissions, nil
}

// ---- table codecs ----

// PermissionsTableCodec renders a resource's permission list as a table.
type PermissionsTableCodec struct{}

// Format reports the codec's output format identifier.
func (c *PermissionsTableCodec) Format() format.Format { return "table" }

// Encode writes the table representation of v to w.
func (c *PermissionsTableCodec) Encode(w io.Writer, v any) error {
	perms, ok := v.([]ResourcePermission)
	if !ok {
		return errors.New("invalid data type for table codec: expected []ResourcePermission")
	}
	t := style.NewTable("PERMISSION", "USER", "TEAM", "BUILT-IN ROLE", "MANAGED", "INHERITED")
	for _, p := range perms {
		user := p.UserLogin
		if user == "" && p.UserID != 0 {
			user = strconv.FormatInt(p.UserID, 10)
		}
		team := p.Team
		if team == "" && p.TeamID != 0 {
			team = strconv.FormatInt(p.TeamID, 10)
		}
		t.Row(p.Permission, user, team, p.BuiltInRole, boolStr(p.IsManaged), boolStr(p.IsInherited))
	}
	return t.Render(w)
}

// Decode is not supported for the table codec.
func (c *PermissionsTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

// DescriptionTableCodec renders a resource's assignable levels as a table.
type DescriptionTableCodec struct{}

// Format reports the codec's output format identifier.
func (c *DescriptionTableCodec) Format() format.Format { return "table" }

// Encode writes the table representation of v to w.
func (c *DescriptionTableCodec) Encode(w io.Writer, v any) error {
	desc, ok := v.(*Description)
	if !ok {
		return errors.New("invalid data type for table codec: expected *Description")
	}
	t := style.NewTable("ASSIGNABLE LEVEL", "ASSIGNMENT TYPES")
	types := assignmentTypes(desc.Assignments)
	for i, level := range desc.Permissions {
		typesCell := ""
		if i == 0 {
			typesCell = strings.Join(types, ", ")
		}
		t.Row(level, typesCell)
	}
	return t.Render(w)
}

// Decode is not supported for the table codec.
func (c *DescriptionTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

func assignmentTypes(a Assignments) []string {
	var out []string
	if a.Users {
		out = append(out, "users")
	}
	if a.ServiceAccounts {
		out = append(out, "serviceAccounts")
	}
	if a.Teams {
		out = append(out, "teams")
	}
	if a.BuiltInRoles {
		out = append(out, "builtInRoles")
	}
	return out
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
