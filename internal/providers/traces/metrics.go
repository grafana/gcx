package traces

import (
	"fmt"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/spf13/cobra"
)

// metricsCmd returns the `metrics` subcommand for TraceQL metrics queries.
func metricsCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var datasource string

	cmd := &cobra.Command{
		Use:   "metrics TRACEQL",
		Short: "Execute a TraceQL metrics query",
		Long: `Execute a TraceQL metrics query against a Tempo datasource.

TRACEQL is the TraceQL metrics expression to evaluate.
Datasource is resolved from -d flag or datasources.tempo in your context.

Instant vs range is deduced from time flags: no time flags = instant query,
--since or --from/--to = range query.`,
		Example: `
  # Instant query (no time flags)
  gcx traces metrics '{ } | rate()'

  # Range query with since
  gcx traces metrics -d tempo-001 '{ } | rate()' --since 1h

  # Range query with explicit time range
  gcx traces metrics '{ } | rate()' --from now-1h --to now --step 30s

  # Output as JSON
  gcx traces metrics -d tempo-001 '{ } | rate()' -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Resolve datasource UID from -d flag, config, or Grafana auto-discovery.
			var cfgCtx *internalconfig.Context
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err == nil {
				cfgCtx = fullCfg.GetCurrentContext()
			}

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "tempo")
			if err != nil {
				return err
			}

			expr := args[0]

			dsType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			if err := dsquery.ValidateDatasourceType(dsType, "tempo"); err != nil {
				return err
			}

			now := time.Now()
			start, end, _, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := tempo.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			// Deduce instant vs range from time flag presence.
			instant := !shared.IsRange()

			step := shared.Step
			if step == "" && !instant {
				step = "60s"
			}

			req := tempo.MetricsRequest{
				Query:   expr,
				Start:   start,
				End:     end,
				Step:    step,
				Instant: instant,
			}

			var resp *tempo.MetricsResponse
			if instant {
				resp, err = client.MetricsInstant(ctx, datasourceUID, req)
			} else {
				resp, err = client.MetricsRange(ctx, datasourceUID, req)
			}
			if err != nil {
				return fmt.Errorf("metrics query failed: %w", err)
			}

			return shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	shared.Setup(cmd.Flags())
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")

	return cmd
}
