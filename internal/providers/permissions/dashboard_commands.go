//nolint:dupl // Folder and dashboard permission commands follow the same shape by design.
package permissions

import (
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// dashboardCommands returns the `dashboard` subcommand group.
func dashboardCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Manage dashboard permissions.",
	}
	cmd.AddCommand(
		newDashboardGetCommand(loader),
		newDashboardUpdateCommand(loader),
	)
	return cmd
}

type dashboardGetOpts struct {
	IO cmdio.Options
}

func (o *dashboardGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &PermissionsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newDashboardGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &dashboardGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <uid>",
		Short: "Get permissions for a dashboard by UID.",
		Args:  cobra.ExactArgs(1),
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

			items, err := client.GetDashboard(ctx, args[0])
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type dashboardUpdateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *dashboardUpdateOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &PermissionsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "file", "f", "", "Path to a JSON file containing the permissions array (or '-' for stdin)")
}

func newDashboardUpdateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &dashboardUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <uid> -f FILE",
		Short: "Update permissions for a dashboard from a JSON file.",
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
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			if err := client.SetDashboard(ctx, args[0], items); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "updated permissions for dashboard %s", args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
