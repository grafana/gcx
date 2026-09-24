package annotations

import "github.com/spf13/cobra"

// NewListCommandForTest exposes newListCommand for external tests.
func NewListCommandForTest(loader RESTConfigLoader) *cobra.Command {
	return newListCommand(loader)
}

func NewCreateCommandForTest(loader RESTConfigLoader) *cobra.Command {
	return newCreateCommand(loader)
}

func NewDeleteCommandForTest(loader RESTConfigLoader) *cobra.Command {
	return newDeleteCommand(loader)
}
