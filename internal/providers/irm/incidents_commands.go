package irm

import (
	"github.com/grafana/gcx/internal/providers/incidents"
	"github.com/spf13/cobra"
)

func newIncidentsCmd(loader *configLoader) *cobra.Command {
	incCmd := &cobra.Command{
		Use:     "incidents",
		Short:   "Manage incidents.",
		Aliases: []string{"incident", "inc"},
	}

	incCmd.AddCommand(
		incidents.NewListCommand(loader),
		incidents.NewGetCommand(loader),
		incidents.NewCreateCommand(loader),
		incidents.NewCloseCommand(loader),
		incidents.NewActivityCommand(loader),
		incidents.NewSeveritiesCommand(loader),
		incidents.NewOpenCommand(loader),
	)

	return incCmd
}
