// Package clusters provides the "gcx instrumentation clusters" command group.
//
// It implements declared/observed state management for K8s monitoring clusters
// following the action-verb CLI design from ADR-018.
//
// Commands:
//
//	list      — enumerate all clusters
//	get       — get a single cluster
//	configure — configure K8s monitoring flags
//	remove    — remove K8s monitoring pipeline
//	wait      — poll until INSTRUMENTED
//	apps      — namespace-level Beyla instrumentation management
package clusters

import (
	"github.com/grafana/gcx/cmd/gcx/instrumentation/clusters/apps"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/spf13/cobra"
)

// Command returns the "clusters" cobra command group and registers all
// cluster-level verb subcommands and the apps subcommand group.
func Command(loader fleet.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "Manage K8s monitoring configuration for clusters",
		Long: `Manage K8s monitoring configuration for clusters.

Subcommands:
  list      List all clusters with their instrumentation status
  get       Show declared config and observed status for a single cluster
  configure Configure K8s monitoring flags (RMW or apply defaults)
  remove    Remove K8s monitoring from a cluster
  wait      Wait until a cluster reaches INSTRUMENTED status
  apps      Manage namespace-level Beyla instrumentation for a cluster`,
	}

	cmd.AddCommand(newListCommand(loader))
	cmd.AddCommand(newGetCommand(loader))
	cmd.AddCommand(newConfigureCommand(loader))
	cmd.AddCommand(newRemoveCommand(loader))
	cmd.AddCommand(newWaitCommand(loader))
	cmd.AddCommand(apps.Command(loader))

	return cmd
}
