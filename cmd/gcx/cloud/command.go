// Package cloud provides the top-level "gcx cloud" command group for managing
// Grafana Cloud platform resources (stacks, orgs, etc.).
package cloud

import (
	"github.com/grafana/gcx/internal/providers/stacks"
	"github.com/spf13/cobra"
)

// Command returns the top-level "cloud" cobra command with all subcommands.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Manage your Grafana Cloud resources",
	}

	cmd.AddCommand(stacks.NewCommand())

	return cmd
}
