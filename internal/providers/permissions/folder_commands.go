//nolint:dupl // Folder and dashboard permission commands follow the same shape by design.
package permissions

import (
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// folderCommands returns the `folder` subcommand group.
func folderCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "folder",
		Short: "Manage folder permissions.",
	}
	cmd.AddCommand(
		newFolderGetCommand(loader),
		newFolderUpdateCommand(loader),
	)
	return cmd
}

type folderGetOpts struct {
	IO cmdio.Options
}

func (o *folderGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &PermissionsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newFolderGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &folderGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <uid>",
		Short: "Get permissions for a folder by UID.",
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

			items, err := client.GetFolder(ctx, args[0])
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type folderUpdateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *folderUpdateOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &PermissionsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "file", "f", "", "Path to a JSON file containing the permissions array (or '-' for stdin)")
}

func newFolderUpdateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &folderUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <uid> -f FILE",
		Short: "Update permissions for a folder from a JSON file.",
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

			if err := client.SetFolder(ctx, args[0], items); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "updated permissions for folder %s", args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
