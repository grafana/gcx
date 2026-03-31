package dashboards

import (
	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/spf13/cobra"
)

// Command returns the dashboards command group.
func Command() *cobra.Command {
	configOpts := &cmdconfig.Options{}

	cmd := &cobra.Command{
		Use:   "dashboards",
		Short: "Render Grafana dashboard snapshots",
		Long:  "Render Grafana dashboards and panels as PNG images via the Image Renderer. For dashboard CRUD operations, use 'gcx resources' with a dashboards selector.",
	}

	configOpts.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(snapshotCmd(configOpts))

	return cmd
}
