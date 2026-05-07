package synth

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// QueryCmd is the `query` subcommand required by the DatasourceProvider
// interface. SM has no single query verb, so this groups the resource-specific
// subcommands (probes, checks) under it. For most use cases the dedicated
// top-level subcommands are friendlier — `gcx datasources synthetic-monitoring probes`
// over `gcx datasources synthetic-monitoring query probes`.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query a Synthetic Monitoring resource through the datasource",
		Long: `Query a Synthetic Monitoring resource through the datasource proxy.
SM has no single query verb; use one of the resource-typed subcommands.
For most use cases, the dedicated top-level subcommands are friendlier:

  gcx datasources synthetic-monitoring probes
  gcx datasources synthetic-monitoring checks`,
	}

	cmd.AddCommand(ProbesCmd(loader))
	cmd.AddCommand(ChecksCmd(loader))

	return cmd
}
