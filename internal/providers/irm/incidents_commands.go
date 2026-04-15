package irm

import (
	"github.com/spf13/cobra"
)

func newIncidentsCmd(loader GrafanaConfigLoader) *cobra.Command {
	incCmd := &cobra.Command{
		Use:     "incidents",
		Short:   "Manage incidents.",
		Aliases: []string{"incident", "inc"},
	}

	incCmd.AddCommand(
		NewListCommand(loader),
		NewGetCommand(loader),
		NewCreateCommand(loader),
		NewCloseCommand(loader),
		NewActivityCommand(loader),
		NewSeveritiesCommand(loader),
		NewOpenCommand(loader),
	)

	return incCmd
}
