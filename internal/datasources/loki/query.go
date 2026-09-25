package loki

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/spf13/cobra"
)

// QueryCmd returns the `query` subcommand for a Loki datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	share := &dsquery.ExploreLinkOpts{}
	drilldown := &dsquery.DrilldownLinkOpts{}
	var limit int
	var datasource string

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a LogQL query against a Loki datasource",
		Long: `Execute a LogQL query against a Loki datasource.

EXPR is the LogQL expression to evaluate.
Datasource is resolved from -d flag or datasources.loki in your context.

Default table output is optimized for humans. Use -o raw for original line
bodies or -o json for the full structured response.

Default --limit is 50; use --limit 0 for no cap.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds. Use --drilldown-link or
--open-drilldown for the equivalent Grafana Logs Drilldown URL (falls back to
the Explore URL for expressions Drilldown's simple filter model can't
represent, e.g. parser stages or aggregations).
Use -o graph for a log-volume-over-time chart — it only charts the lines
--limit actually returned, so pass --limit 0 for the chart to reflect the
full queried range.`,
		Example: `
  # Query logs using configured default datasource
  gcx datasources loki query '{job="varlogs"}'

  # Query with explicit datasource UID
  gcx datasources loki query -d UID '{job="varlogs"} |= "error"'

  # Print a Grafana Explore share link for the query
  gcx datasources loki query '{job="varlogs"}' --share-link

  # Print a Grafana Logs Drilldown link for the query
  gcx datasources loki query '{job="varlogs"}' --drilldown-link

  # Log volume over time, colored by level
  gcx datasources loki query -d UID '{job="varlogs"}' -o graph

  # Raw line bodies only
  gcx datasources loki query -d UID '{job="varlogs"}' -o raw

  # Output as JSON
  gcx datasources loki query -d UID '{job="varlogs"}' -o json`,
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

			datasourceUID, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "loki")
			if err != nil {
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
				Limit: limit,
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if shared.IO.OutputFormat == "graph" && limit != 0 {
				// The log-volume chart only reflects the lines actually
				// fetched. A capped --limit (50 by default) silently caps
				// the chart's apparent volume too, with no other signal
				// that it's a truncated page rather than the true total.
				cmdio.EmitHint(cmd.ErrOrStderr(),
					fmt.Sprintf("-o graph only charts the %d line(s) returned by --limit; pass --limit 0 to chart the full queried range", limit),
					"--limit 0")
			}

			exploreURL := LogsExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
				DatasourceUID:  datasourceUID,
				DatasourceType: dsType,
				Expr:           expr,
				From:           shared.From,
				To:             shared.To,
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

			drilldownURL, _ := LogsDrilldownURL(cfg.GrafanaURL, datasourceUID, expr, start, end)
			drilldownUnavailableMsg, drilldownFailedOpenMsg := dsquery.DrilldownMessages("query", "Logs Drilldown")
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
		agent.AnnotationLLMHint:   `gcx datasources loki query -d UID '{job="grafana"}' -o json`,
	}

	dsquery.RegisterCodecs(&shared.IO, true)
	shared.IO.RegisterCustomCodec("raw", loki.NewRawQueryCodec())
	shared.IO.BindFlags(cmd.Flags())
	shared.SetupTimeFlags(cmd.Flags())
	shared.SetupErrorOnEmptyFlag(cmd.Flags())
	cmd.Flags().StringVar(&shared.Step, "step", "", "Query step (e.g., '15s', '1m')")
	shared.SetupExprFlag(cmd.Flags())
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.loki is configured)")
	cmd.Flags().IntVar(&limit, "limit", dsquery.DefaultLokiLimit, "Maximum number of log lines to return (0 means no limit)")
	share.Setup(cmd.Flags(), "executed query")
	drilldown.Setup(cmd.Flags(), "executed query", "Logs Drilldown")

	return cmd
}
