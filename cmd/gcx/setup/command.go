package setup

import (
	"fmt"

	"github.com/grafana/gcx/cmd/gcx/setup/instrumentation"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// Command returns the setup command area for onboarding and configuring
// Grafana Cloud products.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Onboard and configure Grafana Cloud products.",
	}

	loader := &providers.ConfigLoader{}
	loader.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(instrumentation.Command(loader))
	cmd.AddCommand(statusCmd())

	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show aggregated setup status across all products.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}
}
