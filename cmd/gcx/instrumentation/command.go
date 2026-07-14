// Package instrumentation provides the top-level "gcx instrumentation" command
// and its action-verb subcommand tree for managing Grafana Instrumentation Hub.
//
// The command tree follows the action-verb design from ADR-018:
//
//	gcx instrumentation
//	├── setup      — guided onboarding wizard
//	├── status     — cross-cutting observed-state view
//	├── check      — validate OTel instrumentation locally (otel-checker)
//	├── clusters   — declared/observed state per cluster
//	│   └── apps   — namespace-level RMW operations
//	└── services   — workload-level observed state + overrides
//
// This package does NOT import from internal/providers/instrumentation to avoid a
// cmd → internal/providers cycle. Subcommands are wired here after all
// subcommand packages exist.
package instrumentation

import (
	"github.com/grafana/gcx/cmd/gcx/instrumentation/check"
	"github.com/grafana/gcx/cmd/gcx/instrumentation/clusters"
	"github.com/grafana/gcx/cmd/gcx/instrumentation/services"
	"github.com/grafana/gcx/cmd/gcx/instrumentation/setup"
	"github.com/grafana/gcx/cmd/gcx/instrumentation/status"
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

  setup      Guided onboarding wizard: configures a cluster end-to-end and
             prints a runnable helm install command.

  status     Cross-cutting observed state for clusters and namespaces
             (RunK8sMonitoring + ListPipelines merge).

  check      Validate OpenTelemetry instrumentation for an application
             running locally (env vars, SDK, collector, Beyla, Alloy,
             Grafana Cloud connectivity).

  clusters   Declared and observed state per K8s cluster:
             list, get, configure, remove, wait.
             Sub-group "apps" manages namespace-level Beyla configuration.

  services   Workload-level observed state and per-workload inclusion
             overrides across the fleet: list, get, include, exclude, clear.

Authentication and roles: these commands reach Fleet Management through the
grafana-collector-app plugin proxy using your active Grafana credential (OAuth
included) — no Cloud access-policy token is required. Reads (list/get/status)
need the Viewer role; mutations (setup, configure, remove, include, exclude,
clear, and the K8s discovery/monitoring calls) require the Grafana Admin role.`,
	}

	// Bind --context and --config persistently so all subcommands inherit them.
	loader.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(
		setup.Command(loader),
		status.Command(loader),
		check.Command(),
		clusters.Command(loader),
		services.Command(loader),
	)

	return cmd
}
