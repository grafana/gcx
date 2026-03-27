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
		Long:  "List, inspect, and query Grafana datasources. Supports Prometheus, Loki, Pyroscope, and Tempo datasource-specific query operations alongside generic queries.",
	}

	configOpts.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(listCmd(configOpts))
	cmd.AddCommand(getCmd(configOpts))
	cmd.AddCommand(prometheusCmd(configOpts))
	cmd.AddCommand(lokiCmd(configOpts))
	cmd.AddCommand(pyroscopeCmd(configOpts))
	cmd.AddCommand(tempoCmd())
	cmd.AddCommand(genericCmd(configOpts))

	return cmd
}
