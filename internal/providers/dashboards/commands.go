package dashboards

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/dashboards/search"
	"github.com/grafana/gcx/internal/providers/dashboards/snapshot"
	"github.com/grafana/gcx/internal/providers/dashboards/versions"
	"github.com/spf13/cobra"
)

// commands builds the `gcx dashboards` command subtree.
// All CRUD subcommands share a single loader so config flags are inherited.
func commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	dashCmd := &cobra.Command{
		Use:   "dashboards",
		Short: "Manage Grafana dashboards",
		Long:  "Create, read, update, delete, and search Grafana dashboards via the Kubernetes-compatible Grafana API.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Forward root PersistentPreRun so global flags (logging, color, etc.) apply.
			if root := cmd.Root(); root != cmd && root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	// Bind --config and --context on the parent; all subcommands inherit these.
	loader.BindFlags(dashCmd.PersistentFlags())

	dashCmd.AddCommand(newListCommand(loader))
	dashCmd.AddCommand(newGetCommand(loader))
	dashCmd.AddCommand(newCreateCommand(loader))
	dashCmd.AddCommand(newUpdateCommand(loader))
	dashCmd.AddCommand(newDeleteCommand(loader))
	dashCmd.AddCommand(search.Commands(loader))
	dashCmd.AddCommand(versions.Commands(loader))
	dashCmd.AddCommand(snapshot.Commands(loader))

	return []*cobra.Command{dashCmd}
}
