package instrumentation

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Command returns the instrumentation group command that holds subcommands
// for managing observability instrumentation on Kubernetes clusters.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instrumentation",
		Short: "Manage observability instrumentation for Kubernetes clusters.",
	}

	cmd.AddCommand(statusCmd())
	cmd.AddCommand(discoverCmd())
	cmd.AddCommand(showCmd())
	cmd.AddCommand(applyCmd())

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

func discoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discover",
		Short: "Discover instrumentable workloads in a cluster.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}
}

func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <cluster>",
		Short: "Show current instrumentation config as a portable manifest.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}
}

func applyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Apply an InstrumentationConfig manifest.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}
}
