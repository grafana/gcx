package instances

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// Commands returns the `instances` command group, rooted under `gcx dbo11y`.
// The loader carries the --config flag bound on the dbo11y command; every
// subcommand loads config through it so an explicit --config is honored.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instances",
		Short: "Inspect Database Observability instances discovered from telemetry",
	}
	cmd.AddCommand(newListCommand(loader))
	cmd.AddCommand(newGetCommand(loader))
	return cmd
}
