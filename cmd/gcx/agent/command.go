// Package agent provides agent utility commands for gcx agent mode.
package agent

import (
	skillscmd "github.com/grafana/gcx/cmd/gcx/skills"
	"github.com/spf13/cobra"
)

// Command returns the agent utility command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent mode utilities",
		Long:  "Utilities for gcx agent mode: manage spill files, install and update Agent Skills, and other agent session housekeeping.",
	}
	cmd.AddCommand(pruneCommand())
	cmd.AddCommand(skillscmd.Command())
	return cmd
}
