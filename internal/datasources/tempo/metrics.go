package tempo

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

const defaultTraceMetricsWindow = time.Hour

// MetricsCmd returns the `metrics` subcommand for TraceQL metrics queries.
func MetricsCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	share := &dsquery.ExploreLinkOpts{}
	var datasource string
	var instant bool

	cmd := &cobra.Command{
		Use:   "metrics [TRACEQL]",
		Short: "Execute a TraceQL metrics query",
		Long: `Execute a TraceQL metrics query against a Tempo datasource.

TRACEQL is the TraceQL metrics expression to evaluate.
Datasource is resolved from -d flag or datasources.tempo in your context.

Instant vs range is deduced from time flags: no time flags = instant query,
--since or --from/--to = range query. Use --instant to force an instant query
even when a time range is provided. If no time flags are set, gcx queries the
last hour by default.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		Example: `
  # Instant query over the last hour (default, no time flags)
  gcx datasources tempo metrics '{ } | rate()'

  # Range query with relative window
  gcx datasources tempo metrics -d tempo-001 '{ } | rate()' --since 1h

  # Print a Grafana Explore share link for the query
  gcx datasources tempo metrics '{ } | rate()' --share-link

  # Instant query with explicit time range
  gcx datasources tempo metrics '{ } | rate()' --instant --since 1h

  # Range query with explicit time range and step
  gcx datasources tempo metrics '{ } | rate()' --from now-1h --to now --step 30s

  # Output as JSON
  gcx datasources tempo metrics -d tempo-001 '{ } | rate()' -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			expr, err := shared.ResolveExpr(args, 0)
			if err != nil {
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

			from := shared.From
			to := shared.To
			if from == "" && !req.Start.IsZero() {
				from = req.Start.Format(time.RFC3339)
			}
			if to == "" && !req.End.IsZero() {
				to = req.End.Format(time.RFC3339)
			}
			exploreURL := MetricsExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
				DatasourceUID:  datasourceUID,
				DatasourceType: dsType,
				Expr:           expr,
				From:           from,
				To:             to,
				Instant:        req.Instant,
				OrgID:          dsquery.OrgID(cfgCtx),
			}, 20)
			unavailableMsg, failedOpenMsg := dsquery.ExploreMessages("metrics query")

			return dsquery.EncodeAndHandleExplore(cmd, func() error {
				return shared.IO.Encode(cmd.OutOrStdout(), resp)
			}, *share, dsquery.ExploreLink{
				URL:            exploreURL,
				UnavailableMsg: unavailableMsg,
				FailedOpenMsg:  failedOpenMsg,
			})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources tempo metrics -d UID '{ } | rate()' --since 1h -o json`,
	}

	shared.Setup(cmd.Flags(), true)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	cmd.Flags().BoolVar(&instant, "instant", false, "Run an instant query over the selected time range instead of a range query")
	share.Setup(cmd.Flags(), "executed query")

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
