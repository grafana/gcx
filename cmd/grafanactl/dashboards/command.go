package dashboards

import (
	cmdconfig "github.com/grafana/grafanactl/cmd/grafanactl/config"
	"github.com/spf13/cobra"
)

// Command returns the dashboards command group.
func Command() *cobra.Command {
	configOpts := &cmdconfig.Options{}

	cmd := &cobra.Command{
		Use:   "dashboards",
		Short: "Manage Grafana dashboards",
		Long:  "Capture snapshots and manage Grafana dashboards.",
	}

	configOpts.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(snapshotCmd(configOpts))

	return cmd
}
