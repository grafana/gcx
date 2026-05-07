// Package agent provides agent utility commands for gcx agent mode.
package agent

import (
	"github.com/spf13/cobra"
)

// Command returns the agent utility command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent mode utilities",
		Long:  "Utilities for gcx agent mode: manage spill files and other agent session housekeeping.",
	}
	cmd.AddCommand(pruneCommand())
	return cmd
}
