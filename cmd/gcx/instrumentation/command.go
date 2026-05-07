// Package instrumentation provides the top-level "gcx instrumentation" command
// and its action-verb subcommand tree for managing Grafana Instrumentation Hub.
//
// The command tree follows the action-verb design from ADR-018:
//
//	gcx instrumentation
//	├── setup      — guided onboarding wizard
//	├── status     — cross-cutting observed-state view
//	├── clusters   — declared/observed state per cluster
//	│   └── apps   — namespace-level RMW operations
//	└── services   — workload-level observed state + overrides
//
// This package does NOT import from internal/providers/instrumentation to avoid a
// cmd → internal/providers cycle. Subcommands are wired here after all
// subcommand packages exist.
package instrumentation

import (
	"github.com/grafana/gcx/cmd/gcx/instrumentation/clusters"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// Command returns the top-level "instrumentation" cobra command with all
// subcommands registered. The loader is created here and bound to
// PersistentFlags so --context and --config are available to all subcommands.
func Command() *cobra.Command {
	loader := &providers.ConfigLoader{}

	cmd := &cobra.Command{
		Use:   "instrumentation",
		Short: "Manage Grafana Instrumentation Hub",
		Long: `Manage Grafana Instrumentation Hub using action-verb commands.

The instrumentation command tree provides:

  clusters   Declared and observed state per K8s cluster:
             list, get, configure, remove, wait.
             Sub-group "apps" manages namespace-level Beyla configuration.`,
	}

	// Bind --context and --config persistently so all subcommands inherit them.
	loader.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(
		clusters.Command(loader),
	)

	return cmd
}
