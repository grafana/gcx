package datasources

import (
	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/spf13/cobra"
)

// Command returns the datasources command group.
func Command() *cobra.Command {
	configOpts := &cmdconfig.Options{}

	cmd := &cobra.Command{
		Use:   "datasources",
		Short: "Manage and query Grafana datasources",
		Long:  "List, inspect, and query Grafana datasources. Use top-level signal commands (metrics, logs, traces, profiles) for datasource-specific queries.",
	}

	configOpts.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(listCmd(configOpts))
	cmd.AddCommand(getCmd(configOpts))
	cmd.AddCommand(queryCmd(configOpts))

	return cmd
}
