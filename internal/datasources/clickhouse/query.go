package clickhouse

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/spf13/cobra"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

// QueryCmd returns the `query` subcommand for a ClickHouse datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	share := &dsquery.ExploreLinkOpts{}
	var datasource string
	var limit int

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a SQL query against a ClickHouse datasource",
		Long: `Execute a SQL query against a ClickHouse datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.clickhouse in your context.
Server-side macros ($__timeFilter, $__timeInterval, etc.) are supported.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		Example: `
  # Simple query
  gcx datasources clickhouse query 'SELECT count() FROM events'

  # With time macro and explicit datasource
  gcx datasources clickhouse query -d UID 'SELECT * FROM logs WHERE $__timeFilter(timestamp)' --since 1h

  # Output as JSON
  gcx datasources clickhouse query -d UID 'SELECT 1' -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources clickhouse query 'SELECT 1' --share-link

  # Disable limit enforcement
  gcx datasources clickhouse query 'SELECT * FROM big_table' --limit 0`,
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

			datasourceUID, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "clickhouse")
			if err != nil {
				return err
			}

			sql := clickhouse.EnforceLimit(expr, limit, maxLimit)

			now := time.Now()
			start, end, step, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			var intervalMs int64
			if step > 0 {
				intervalMs = step.Milliseconds()
			}

			client, err := clickhouse.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, clickhouse.QueryRequest{
				RawSQL:     sql,
				Start:      start,
				End:        end,
				IntervalMs: intervalMs,
			})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			exploreURL := QueryExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
				DatasourceUID:  datasourceUID,
				DatasourceType: dsType,
				Expr:           sql,
				From:           shared.From,
				To:             shared.To,
				OrgID:          dsquery.OrgID(cfgCtx),
			})
			unavailableMsg, failedOpenMsg := dsquery.ExploreMessages("query")

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
		agent.AnnotationLLMHint:   `gcx datasources clickhouse query -d UID 'SELECT count() FROM events' -o json`,
	}

	shared.Setup(cmd.Flags(), false)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.clickhouse is configured)")
	cmd.Flags().IntVar(&limit, "limit", defaultLimit, "Max rows to return (0 disables enforcement)")
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
