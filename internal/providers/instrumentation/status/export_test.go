package status

import (
	"github.com/grafana/gcx/internal/fleet"
	"github.com/spf13/cobra"
)

// NewCommandForTest exposes newCommand for use in external test packages.
func NewCommandForTest(loader fleet.ConfigLoader) *cobra.Command {
	return newCommand(loader)
}
