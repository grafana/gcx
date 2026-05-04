package synth

import (
	"fmt"

	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// QueryCmd is the `query` subcommand required by the DatasourceProvider
// interface. SM has no single query verb, so this just routes to the
// resource-specific subcommands (probes, checks). For most use cases the
// dedicated subcommands are friendlier — `gcx datasources synth probes`
// over `gcx datasources synth query probes`.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query <probes|checks>",
		Short: "Query a Synthetic Monitoring resource through the datasource",
		Long: `Query a Synthetic Monitoring resource through the datasource proxy.
SM has no single query verb; pass "probes" or "checks" to target a resource type.
For most use cases, the dedicated subcommands are friendlier:

  gcx datasources synth probes
  gcx datasources synth checks`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "probes":
				return ProbesCmd(loader).RunE(cmd, nil)
			case "checks":
				return ChecksCmd(loader).RunE(cmd, nil)
			default:
				return fmt.Errorf("unknown synthetic-monitoring resource %q: supported are probes, checks", args[0])
			}
		},
	}

	// Bind the same flags the resource subcommands use so -d works at this level too.
	opts := &probesOpts{}
	opts.setup(cmd.Flags())

	return cmd
}
