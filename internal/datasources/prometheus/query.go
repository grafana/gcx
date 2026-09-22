package prometheus

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/spf13/cobra"
)

// QueryCmd returns the `query` subcommand for a Prometheus datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return QueryCmdWithDefault(loader, "")
}

// QueryCmdWithDefault returns the query command with a fallback datasource
// UID used when --datasource is not provided. Pass "" for no default.
func QueryCmdWithDefault(loader *providers.ConfigLoader, defaultDS string) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	share := &dsquery.ExploreLinkOpts{}
	drilldown := &dsquery.DrilldownLinkOpts{}
	var datasource string

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a PromQL query against a Prometheus datasource",
		Long: `Execute a PromQL query against a Prometheus datasource.

EXPR is the PromQL expression to evaluate, passed as a positional argument or
via --expr (familiar to promtool users).
Datasource is resolved from -d flag or datasources.prometheus in your context.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds. Use --drilldown-link or
--open-drilldown for the equivalent Grafana Metrics Drilldown URL (only
available for a bare metric or a single function/aggregation wrapper around
one metric; expressions referencing more than one metric fall back to the
Explore URL).`,
		Example: `
  # Instant query using configured default datasource
  gcx datasources prometheus query 'up{job="grafana"}'

  # Instant query at a specific time
  gcx datasources prometheus query 'rate(http_requests_total[5m])' --time 2026-01-15T10:30:00Z

  # Range query with explicit datasource UID
  gcx datasources prometheus query -d UID 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m

  # Query the last hour
  gcx datasources prometheus query 'up' --since 1h

  # Print a Grafana Explore share link for the executed query
  gcx datasources prometheus query 'up' --share-link

  # Print a Grafana Metrics Drilldown link for the executed query
  gcx datasources prometheus query 'rate(http_requests_total[5m])' --drilldown-link

  # Output as JSON
  gcx datasources prometheus query -d UID 'up' -o json`,
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
			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			effectiveDS := datasource
			if effectiveDS == "" {
				effectiveDS = defaultDS
			}

			datasourceUID, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, effectiveDS, cfgCtx, cfg, "prometheus")
			if err != nil {
				return err
			}

			now := time.Now()
			start, end, step, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			// --time: instant query at a specific timestamp
			instant := shared.Time != ""
			if instant {
				t, err := dsquery.ParseTime(shared.Time, now)
				if err != nil {
					return fmt.Errorf("invalid --time value: %w", err)
				}
				start = t
			}

			client, err := prometheus.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := prometheus.QueryRequest{
				Query:   expr,
				Start:   start,
				End:     end,
				Step:    step,
				Instant: instant,
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}
			exploreURL := QueryExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
				DatasourceUID:  datasourceUID,
				DatasourceType: dsType,
				Expr:           expr,
				From:           shared.From,
				To:             shared.To,
				Instant:        !req.IsRange(),
				Step:           step,
				OrgID:          dsquery.OrgID(cfgCtx),
			})
			unavailableMsg, failedOpenMsg := dsquery.ExploreMessages("query")

			if err := dsquery.EncodeAndHandleExplore(cmd, func() error {
				return shared.IO.Encode(cmd.OutOrStdout(), resp)
			}, *share, dsquery.ExploreLink{
				URL:            exploreURL,
				UnavailableMsg: unavailableMsg,
				FailedOpenMsg:  failedOpenMsg,
			}); err != nil {
				return err
			}

			drilldownURL, _ := MetricsDrilldownURL(cfg.GrafanaURL, datasourceUID, expr, start, end)
			drilldownUnavailableMsg, drilldownFailedOpenMsg := dsquery.DrilldownMessages("query", "Metrics Drilldown")
			if err := dsquery.HandleDrilldownLinkWithExploreFallback(cmd, *drilldown, drilldownURL, drilldownUnavailableMsg, drilldownFailedOpenMsg,
				share.Enabled(), exploreURL, unavailableMsg, failedOpenMsg); err != nil {
				return err
			}

			if shared.ErrorOnEmpty {
				return dsquery.ErrorOnEmptyWithContext(resp, dsquery.EmptyResultContext{
					Expr: expr, DatasourceUID: datasourceUID, Start: start, End: end,
				})
			}
			return nil
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources prometheus query -d UID 'up{job="grafana"}' -o json`,
	}

	shared.Setup(cmd.Flags(), true)
	shared.SetupErrorOnEmptyFlag(cmd.Flags())
	shared.SetupInstantFlag(cmd.Flags())
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.prometheus is configured)")
	share.Setup(cmd.Flags(), "executed query")
	drilldown.Setup(cmd.Flags(), "executed query", "Metrics Drilldown")

	return cmd
}
