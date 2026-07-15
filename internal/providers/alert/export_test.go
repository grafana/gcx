package alert

import "github.com/spf13/cobra"

// RulerSubtypeForDatasourceType exposes the subtype mapping to tests.
func RulerSubtypeForDatasourceType(dsType string) (string, error) {
	return rulerSubtypeForDatasourceType(dsType)
}

// RulerCommands exposes the ruler command tree to tests.
func RulerCommands(loader GrafanaConfigLoader) *cobra.Command {
	return rulerCommands(loader)
}
