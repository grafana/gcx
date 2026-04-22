package setup

import (
	"fmt"
	"io"

	fleetbase "github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/providers"
	instrum "github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Command returns the setup command area for onboarding and configuring
// Grafana Cloud products.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Onboard and configure Grafana Cloud products.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root != nil && root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader := &providers.ConfigLoader{}
	loader.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(newStatusCommand(loader))

	return cmd
}

type setupStatusOpts struct{}

func (o *setupStatusOpts) setup(_ *pflag.FlagSet) {}

func (o *setupStatusOpts) Validate() error { return nil }

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
	t := style.NewTable("PRODUCT", "ENABLED", "HEALTH", "DETAILS")
	for _, r := range rows {
		t.Row(r.Product, r.Enabled, r.Health, r.Details)
	}
	return t.Render(w)
}
