package instrumentation

import (
	"github.com/grafana/gcx/internal/fleet"
	"github.com/spf13/cobra"
)

// Command returns the instrumentation group command that holds subcommands
// for managing observability instrumentation on Kubernetes clusters.
func Command(loader fleet.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instrumentation",
		Short: "Manage observability instrumentation for Kubernetes clusters.",
	}

	cmd.AddCommand(newStatusCommand(loader))
	cmd.AddCommand(newDiscoverCommand(loader))
	cmd.AddCommand(newShowCommand(loader))
	cmd.AddCommand(newApplyCommand(loader))

	return cmd
}
