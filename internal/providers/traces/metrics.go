package traces

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

const defaultTraceMetricsWindow = time.Hour

// metricsCmd returns the `metrics` subcommand for TraceQL metrics queries.
func metricsCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var datasource string
	var instant bool

	cmd := &cobra.Command{
		Use:   "metrics TRACEQL",
		Short: "Execute a TraceQL metrics query",
		Long: `Execute a TraceQL metrics query against a Tempo datasource.

TRACEQL is the TraceQL metrics expression to evaluate.
Datasource is resolved from -d flag or datasources.tempo in your context.

Instant vs range is deduced from time flags: no time flags = instant query,
--since or --from/--to = range query. Use --instant to force an instant query
even when a time range is provided. If no time flags are set, gcx queries the
last hour by default.`,
		Example: `
  # Instant query over the last hour (default, no time flags)
  gcx traces metrics '{ } | rate()'

  # Range query with relative window
  gcx traces metrics -d tempo-001 '{ } | rate()' --since 1h

  # Instant query with explicit time range
  gcx traces metrics '{ } | rate()' --instant --since 1h

  # Range query with explicit time range and step
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
			if err != nil {
				logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
			} else {
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
			req, err := buildMetricsRequest(expr, shared, instant, now)
			if err != nil {
				return err
			}

			client, err := tempo.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			var resp *tempo.MetricsResponse
			if req.Instant {
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

	shared.Setup(cmd.Flags(), true)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	cmd.Flags().BoolVar(&instant, "instant", false, "Run an instant query over the selected time range instead of a range query")

	return cmd
}

func buildMetricsRequest(expr string, shared *dsquery.SharedOpts, instantFlag bool, now time.Time) (tempo.MetricsRequest, error) {
	// Infer instant from time flag absence, consistent with how metrics query (Prometheus) works.
	instant := instantFlag || !shared.IsRange()

	if instant && shared.Step != "" {
		return tempo.MetricsRequest{}, errors.New("--step is not supported with --instant")
	}

	start, end, _, err := shared.ParseTimes(now)
	if err != nil {
		return tempo.MetricsRequest{}, err
	}
	if start.IsZero() && end.IsZero() {
		end = now
		start = now.Add(-defaultTraceMetricsWindow)
	}

	step := shared.Step
	if step == "" && !instant {
		step = "60s"
	}

	return tempo.MetricsRequest{
		Query:   expr,
		Start:   start,
		End:     end,
		Step:    step,
		Instant: instant,
	}, nil
}
