package instrumentation

import (
	"fmt"

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

	cmd.AddCommand(statusCmd())
	cmd.AddCommand(newDiscoverCommand(loader))
	cmd.AddCommand(newShowCommand(loader))
	cmd.AddCommand(newApplyCommand(loader))

	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show instrumentation status across clusters.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}
}


