package synth

import (
	"encoding/json"
	"fmt"
	"log/slog"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type checksOpts struct {
	IO         cmdio.Options
	Datasource string
	WithAlerts bool
}

func (opts *checksOpts) setup(flags *pflag.FlagSet) {
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless default-synth-datasource is configured)")
	flags.BoolVar(&opts.WithAlerts, "with-alerts", false, "Include each check's alert rules in the response (server-side composition via ?includeAlerts=true)")
}

func (opts *checksOpts) Validate() error {
	return opts.IO.Validate()
}

// ChecksCmd returns the `checks` subcommand for a Synthetic Monitoring datasource parent.
func ChecksCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &checksOpts{}

	cmd := &cobra.Command{
		Use:   "checks",
		Short: "List Synthetic Monitoring checks",
		Long:  "List all checks accessible through the configured Synthetic Monitoring datasource.",
		Example: `
  # List checks (use datasource UID, not name)
  gcx datasources synth checks -d UID

  # List checks with their alert rules embedded (one server-side call)
  gcx datasources synth checks -d UID --with-alerts

  # Output as JSON
  gcx datasources synth checks -d UID -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			var cfgCtx *internalconfig.Context
			if fullCfg, err := loader.LoadFullConfig(ctx); err == nil {
				cfgCtx = fullCfg.GetCurrentContext()
			} else {
				logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "synth")
			if err != nil {
				return err
			}

			client, err := synth.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			var result []checks.Check
			if opts.WithAlerts {
				result, err = client.ListChecksWithAlerts(ctx, datasourceUID)
			} else {
				result, err = client.ListChecks(ctx, datasourceUID)
			}
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}
