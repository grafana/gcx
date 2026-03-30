package setup

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/grafana/gcx/cmd/gcx/setup/instrumentation"
	fleetbase "github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/providers"
	instrum "github.com/grafana/gcx/internal/setup/instrumentation"
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
	cmd.AddCommand(statusCmd(loader))

	return cmd
}

func statusCmd(loader *providers.ConfigLoader) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show aggregated setup status across all products.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			base, _, err := fleetbase.LoadClient(ctx, loader)
			if err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}
			client := instrum.NewClient(base)

			monResp, err := client.RunK8sMonitoring(ctx)
			if err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}

			enabled := "no"
			if len(monResp.Clusters) > 0 {
				enabled = "yes"
			}
			details := fmt.Sprintf("%d clusters", len(monResp.Clusters))

			return writeSetupStatusTable(cmd.OutOrStdout(), []setupProductRow{
				{Product: "instrumentation", Enabled: enabled, Health: "healthy", Details: details},
			})
		},
	}
}

type setupProductRow struct {
	Product string
	Enabled string
	Health  string
	Details string
}

func writeSetupStatusTable(w io.Writer, rows []setupProductRow) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PRODUCT\tENABLED\tHEALTH\tDETAILS")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Product, r.Enabled, r.Health, r.Details)
	}
	return tw.Flush()
}
