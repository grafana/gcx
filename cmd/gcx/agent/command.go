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
		Short: "Utilities for AI agents",
		Long:  "Utilities for AI agents: send phone notifications, manage spill files, and install and update Agent Skills.",
	}
	cmd.AddCommand(pingCommand())
	cmd.AddCommand(pruneCommand())
	cmd.AddCommand(skillscmd.Command())
	return cmd
}
