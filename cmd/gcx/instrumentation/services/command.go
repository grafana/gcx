// Package services provides the "gcx instrumentation services" command tree
// for managing workload-level observed state and per-workload overrides.
//
// The command tree implements ADR-018 action-verb design:
//
//	gcx instrumentation services
//	├── list     — fleet-wide RunK8sDiscovery with client-side filters
//	├── get      — single workload lookup by (cluster, namespace, service)
//	├── include  — DWIM per-workload include override
//	├── exclude  — DWIM per-workload exclude override
//	└── clear    — remove any per-workload override
package services

import (
	"github.com/grafana/gcx/internal/fleet"
	"github.com/spf13/cobra"
)

// Command returns the "services" cobra command with list/get/include/exclude/clear
// subcommands registered.
func Command(loader fleet.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "services",
		Short: "Manage workload-level instrumentation across the fleet",
		Long: `Manage workload-level observed state and per-workload inclusion
overrides across the fleet.

Subcommands:

  list     List all discovered workloads, with optional filtering.
  get      Get a single workload by (cluster, namespace, service).
  include  Include a workload for instrumentation (DWIM, idempotent).
  exclude  Exclude a workload from instrumentation (DWIM, idempotent).
  clear    Remove a per-workload override, inheriting namespace default.`,
	}

	cmd.AddCommand(newListCommand(loader))
	cmd.AddCommand(newGetCommand(loader))
	cmd.AddCommand(newIncludeCommand(loader))
	cmd.AddCommand(newExcludeCommand(loader))
	cmd.AddCommand(newClearCommand(loader))

	return cmd
}
