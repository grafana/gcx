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
	"github.com/spf13/pflag"
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
	cmd.AddCommand(newStatusCommand(loader))

	return cmd
}

type setupStatusOpts struct {
	IO setupStatusIO
}

func (o *setupStatusOpts) setup(flags *pflag.FlagSet) {
	_ = flags // no flags yet — placeholder for future --output support
}

func (o *setupStatusOpts) Validate() error {
	return nil
}

// setupStatusIO is a minimal output interface for the aggregated status table.
// Kept separate from output.Options since aggregated status has a fixed table format.
type setupStatusIO struct{}

func newStatusCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &setupStatusOpts{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show aggregated setup status across all products.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			r, err := fleetbase.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("setup: %w", err)
			}
			client := instrum.NewClient(r.Client)
			promHdrs := instrum.PromHeadersFromStack(r.Stack)

			monResp, err := client.RunK8sMonitoring(ctx, promHdrs)
			if err != nil {
				return fmt.Errorf("setup: %w", err)
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
	opts.setup(cmd.Flags())
	return cmd
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
