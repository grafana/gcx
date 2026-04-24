package loki

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

// MetricsCmd returns the `metrics` subcommand for metric LogQL queries.
func MetricsCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	share := &dsquery.ExploreLinkOpts{}
	var datasource string

	cmd := &cobra.Command{
		Use:   "metrics [EXPR]",
		Short: "Execute a metric LogQL query against a Loki datasource",
		Long: `Execute a metric LogQL query and return time-series results.

EXPR is a metric LogQL expression (e.g., rate, count_over_time, sum).
Datasource is resolved from -d flag or datasources.loki in your context.

Unlike 'logs query' which returns log lines, 'logs metrics' returns
time-series data with proper table, graph, and JSON formatters.

Instant vs range is deduced from time flags: no time flags = instant query,
--since or --from/--to = range query.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		Example: `
  # Rate of log lines over 5 minutes
  gcx datasources loki metrics 'rate({job="varlogs"}[5m])' --since 1h -o table

  # Count of error logs
  gcx datasources loki metrics 'count_over_time({job="varlogs"} |= "error" [5m])' --since 1h

  # Print a Grafana Explore share link for the query
  gcx datasources loki metrics 'rate({job="varlogs"}[5m])' --share-link

  # Line chart output
  gcx datasources loki metrics -d loki-001 'rate({job="varlogs"}[5m])' --since 1h -o graph

  # Output as JSON
  gcx datasources loki metrics 'rate({job="varlogs"}[5m])' --since 1h -o json`,
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

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "loki")
			if err != nil {
				return err
			}

			dsType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			if err := dsquery.ValidateDatasourceType(dsType, "loki"); err != nil {
				return err
			}

			now := time.Now()
			start, end, step, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := loki.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := loki.QueryRequest{
				Query: expr,
				Start: start,
				End:   end,
				Step:  step,
			}

			resp, err := client.MetricQuery(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("metric query failed: %w", err)
			}

			exploreURL := MetricsExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
				DatasourceUID:  datasourceUID,
				DatasourceType: dsType,
				Expr:           expr,
				From:           shared.From,
				To:             shared.To,
				Instant:        !req.IsRange(),
				OrgID:          dsquery.OrgID(cfgCtx),
			})
			unavailableMsg, failedOpenMsg := dsquery.ExploreMessages("metric query")

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
		agent.AnnotationLLMHint:   `gcx datasources loki metrics -d UID 'rate({job="grafana"}[5m])' --since 1h -o json`,
	}

	shared.Setup(cmd.Flags(), true)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.loki is configured)")
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
