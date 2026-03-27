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
		Short: "Manage Grafana datasources",
		Long:  "List, inspect, and generically query Grafana datasources.",
	}

	configOpts.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(listCmd(configOpts))
	cmd.AddCommand(getCmd(configOpts))
	cmd.AddCommand(genericCmd(configOpts))

	return cmd
}

// PrometheusCommand returns the top-level Prometheus datasource command group.
func PrometheusCommand() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	cmd := prometheusCmd(configOpts)
	configOpts.BindFlags(cmd.PersistentFlags())
	return cmd
}

// LokiCommand returns the top-level Loki datasource command group.
func LokiCommand() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	cmd := lokiCmd(configOpts)
	configOpts.BindFlags(cmd.PersistentFlags())
	return cmd
}

// PyroscopeCommand returns the top-level Pyroscope datasource command group.
func PyroscopeCommand() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	cmd := pyroscopeCmd(configOpts)
	configOpts.BindFlags(cmd.PersistentFlags())
	return cmd
}

// TempoCommand returns the top-level Tempo datasource command group.
func TempoCommand() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	cmd := tempoCmd()
	configOpts.BindFlags(cmd.PersistentFlags())
	return cmd
}
