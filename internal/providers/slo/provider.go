package slo

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/grafana/gcx/internal/providers/slo/reports"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(NewSLOProvider())
}

// shortDesc is the SLO provider's one-line description, shared by the cobra
// command tree and adapter.NewProvider.
const shortDesc = "Manage Grafana SLO definitions and reports"

// NewSLOProvider registers SLO definitions and reports through their resource
// declarations and attaches the product command tree.
func NewSLOProvider() *adapter.Provider {
	return adapter.NewProvider("slo", shortDesc, providers.LoadGrafanaDeps, definitions.SloResource(), reports.ReportResource()).
		WithCommands(newSLOCommands)
}

func newSLOCommands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	sloCmd := &cobra.Command{
		Use:   "slo",
		Short: shortDesc,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	// Bind config flags on the parent — all subcommands inherit these.
	loader.BindFlags(sloCmd.PersistentFlags())

	sloCmd.AddCommand(definitions.Commands(loader))
	sloCmd.AddCommand(reports.Commands(loader))

	return []*cobra.Command{sloCmd}
}
