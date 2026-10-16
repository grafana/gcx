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

// graphLimitHint explains when a -o graph chart may not cover the whole queried
// range: --limit 0 only omits maxLines (the backend default still applies),
// and a result that reached --limit was truncated.
func graphLimitHint(limit, returned int) (string, bool) {
	switch {
	case limit == 0:
		return fmt.Sprintf("-o graph charts the %d line(s) returned; --limit 0 omits the limit, so Loki's own default line limit may apply and the chart may not cover the whole range", returned), true
	case returned >= limit:
		return fmt.Sprintf("-o graph only charts the %d line(s) returned (capped by --limit); raise --limit to include more, though the backend may enforce its own maximum", returned), true
	}
	return "", false
}

func countEntries(resp *loki.QueryResponse) int {
	n := 0
	for _, stream := range resp.Data.Result {
		n += len(stream.Values)
	}
	return n
}

// QueryCmd returns the `query` subcommand for a Loki datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	share := &dsquery.ExploreLinkOpts{}
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

Default --limit is 50. --limit 0 omits the limit, so Loki's own default
applies (100 lines in Loki's range-query API).
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.
Use -o graph for a log-volume-over-time chart of the lines the query
returned, counted per level. It does not cover lines beyond --limit or the
backend's own line limits, so it can undercount a busy time range.`,
		Example: `
  # Query logs using configured default datasource
  gcx datasources loki query '{job="varlogs"}'

  # Query with explicit datasource UID
  gcx datasources loki query -d UID '{job="varlogs"} |= "error"'

  # Print a Grafana Explore share link for the query
  gcx datasources loki query '{job="varlogs"}' --share-link

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

			if shared.IO.OutputFormat == "graph" {
				if summary, ok := graphLimitHint(limit, countEntries(resp)); ok {
					cmdio.EmitHint(cmd.ErrOrStderr(), summary, "")
				}
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

			resultErr := dsquery.EncodeAndHandleExplore(cmd, func() error {
				return shared.IO.Encode(cmd.OutOrStdout(), resp)
			}, *share, dsquery.ExploreLink{
				URL:            exploreURL,
				UnavailableMsg: unavailableMsg,
				FailedOpenMsg:  failedOpenMsg,
			})
			if resultErr != nil {
				return resultErr
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
	cmd.Flags().IntVar(&limit, "limit", dsquery.DefaultLokiLimit, "Maximum number of log lines to return (0 omits the limit, so Loki's default applies)")
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
