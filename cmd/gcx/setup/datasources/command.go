// Package datasources provides the `gcx setup datasources <cloud>` command
// family, which discovers cloud resources via the native cloud CLI and
// provisions matching Grafana datasources.
package datasources

import (
	azurecmd "github.com/grafana/gcx/cmd/gcx/setup/datasources/azure"
	"github.com/spf13/cobra"
)

// Command returns the `gcx setup datasources` command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "datasources",
		Short: "Set up cloud provider datasources",
		Long: "Discover cloud resources using your local cloud CLI session and " +
			"provision the matching Grafana datasources, minting dedicated, " +
			"gcx-owned credentials per datasource.\n\n" +
			"Azure is supported today (Azure Monitor, Azure Data Explorer, and Azure CosmosDB). " +
			"AWS and GCP are planned.",
	}

	cmd.AddCommand(azurecmd.Command())

	return cmd
}
