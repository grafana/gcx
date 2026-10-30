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

func NewMassDeleteCommandForTest(loader RESTConfigLoader) *cobra.Command {
	return newMassDeleteCommand(loader)
}
func NewUpdateCommandForTest(loader RESTConfigLoader) *cobra.Command { return newUpdateCommand(loader) }
