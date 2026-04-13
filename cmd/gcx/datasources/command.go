package datasources

import (
	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/providers"
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
	cmd.AddCommand(QueryCmd(configOpts))

	for _, dp := range datasources.AllProviders() {
		loader := &providers.ConfigLoader{}
		sub := &cobra.Command{
			Use:   dp.Kind(),
			Short: dp.ShortDesc(),
			PersistentPreRun: func(cmd *cobra.Command, args []string) {
				if root := cmd.Root(); root.PersistentPreRun != nil {
					root.PersistentPreRun(cmd, args)
				}
			},
		}
		loader.BindFlags(sub.PersistentFlags())
		sub.AddCommand(dp.QueryCmd(loader))
		for _, extra := range dp.ExtraCommands(loader) {
			sub.AddCommand(extra)
		}
		cmd.AddCommand(sub)
	}

	return cmd
}
