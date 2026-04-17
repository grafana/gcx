package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/grafana/gcx/internal/config"
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

// resourceKind describes one permission-bearing resource type (folder or dashboard).
type resourceKind struct {
	// name is the user-facing singular noun (e.g. "folder").
	name string
	// get retrieves permissions for the resource with the given UID.
	get func(c *Client, ctx context.Context, uid string) ([]Item, error)
	// set replaces permissions for the resource with the given UID.
	set func(c *Client, ctx context.Context, uid string, items []Item) error
}

//nolint:gochecknoglobals // immutable resource-kind dispatch tables.
var (
	folderKind = resourceKind{
		name: "folder",
		get:  (*Client).GetFolder,
		set:  (*Client).SetFolder,
	}
	dashboardKind = resourceKind{
		name: "dashboard",
		get:  (*Client).GetDashboard,
		set:  (*Client).SetDashboard,
	}
)

// resourceCommands returns the Cobra group (get/update) for a resource kind.
func resourceCommands(loader GrafanaConfigLoader, kind resourceKind) *cobra.Command {
	cmd := &cobra.Command{
		Use:   kind.name,
		Short: fmt.Sprintf("Manage %s permissions.", kind.name),
	}
	cmd.AddCommand(
		newGetCommand(loader, kind),
		newUpdateCommand(loader, kind),
	)
	return cmd
}

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &PermissionsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader GrafanaConfigLoader, kind resourceKind) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <uid>",
		Short: fmt.Sprintf("Get permissions for a %s by UID.", kind.name),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := newClientFromLoader(ctx, loader)
			if err != nil {
				return err
			}

			items, err := kind.get(client, ctx, args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type updateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &PermissionsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "file", "f", "", "Path to a JSON file containing the permissions array (or '-' for stdin)")
}

func newUpdateCommand(loader GrafanaConfigLoader, kind resourceKind) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <uid> -f FILE",
		Short: fmt.Sprintf("Update permissions for a %s from a JSON file.", kind.name),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			items, err := readItemsFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := newClientFromLoader(ctx, loader)
			if err != nil {
				return err
			}

			if err := kind.set(client, ctx, args[0], items); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "updated permissions for %s %s", kind.name, args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newClientFromLoader(ctx context.Context, loader GrafanaConfigLoader) (*Client, error) {
	restCfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, err
	}
	return NewClient(restCfg)
}

// readItemsFromFile reads a JSON permissions payload from path. When path is "-"
// it reads from stdin. The input may be either a bare JSON array of Items or an
// object shaped like {"items": [...]}.
func readItemsFromFile(path string, stdin io.Reader) ([]Item, error) {
	if path == "" {
		return nil, errors.New("--file is required")
	}

	var (
		data []byte
		err  error
	)
	if path == "-" {
		if stdin == nil {
			stdin = os.Stdin
		}
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read permissions file: %w", err)
	}

	return parseItems(data)
}

// parseItems accepts either a bare JSON array or a {"items":[…]} envelope.
func parseItems(data []byte) ([]Item, error) {
	var items []Item
	if err := json.Unmarshal(data, &items); err == nil {
		return items, nil
	}

	var envelope setBody
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse permissions: expected JSON array or object with 'items' field: %w", err)
	}
	return envelope.Items, nil
}

func permissionName(p int) string {
	switch p {
	case PermissionView:
		return "View"
	case PermissionEdit:
		return "Edit"
	case PermissionAdmin:
		return "Admin"
	default:
		return strconv.Itoa(p)
	}
}

// PermissionsTableCodec renders a slice of permission items as a table.
type PermissionsTableCodec struct{}

// Format reports the codec's output format identifier.
func (c *PermissionsTableCodec) Format() format.Format { return "table" }

// Encode writes the table representation of v to w.
func (c *PermissionsTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]Item)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Item")
	}

	t := style.NewTable("ROLE", "USER", "TEAM", "PERMISSION")
	for _, it := range items {
		team := ""
		if it.TeamID != 0 {
			team = strconv.Itoa(it.TeamID)
		}
		t.Row(it.Role, it.UserLogin, team, permissionName(it.Permission))
	}
	return t.Render(w)
}

// Decode is not supported for the table codec.
func (c *PermissionsTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}
