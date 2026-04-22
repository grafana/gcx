package annotations

import "github.com/spf13/cobra"

// NewListCommandForTest exposes newListCommand for external tests.
func NewListCommandForTest(loader RESTConfigLoader) *cobra.Command {
	return newListCommand(loader)
}
