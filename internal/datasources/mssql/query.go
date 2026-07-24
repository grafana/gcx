package mssql

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/mssql"
	"github.com/spf13/cobra"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

// QueryCmd returns the `query` subcommand for an MSSQL datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	share := &dsquery.ExploreLinkOpts{}
	var datasource string
	var limit int

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a SQL query against an MSSQL datasource",
		Long: `Execute a T-SQL query against a Microsoft SQL Server datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.mssql in your context.
Server-side macros ($__timeFilter, $__timeGroup, etc.) are supported.

T-SQL has no LIMIT keyword. By default the result is capped with an injected
TOP (n) clause (see --limit); use --limit 0 to disable it, or write your own
TOP / OFFSET ... FETCH. Injection only applies to simple leading-SELECT
statements — CTEs (WITH), set operations (UNION/INTERSECT/EXCEPT), queries that
already use TOP, and OFFSET/FETCH queries are left unchanged.

Use --share-link to print the equivalent Grafana Explore URL, or --open to open
it in your browser after the query succeeds.`,
		Example: `
  # Simple query (capped at TOP (100))
  gcx datasources mssql query 'SELECT name, id FROM dbo.WORLD_DATA'

  # With time macro and explicit datasource
  gcx datasources mssql query -d UID 'SELECT * FROM events WHERE $__timeFilter(created_at)' --since 1h

  # Cap at 10 rows (injects TOP (10))
  gcx datasources mssql query -d UID 'SELECT * FROM dbo.WORLD_DATA' --limit 10

  # Disable TOP injection and output JSON
  gcx datasources mssql query 'SELECT * FROM dbo.WORLD_DATA' --limit 0 -o json`,
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

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			// Resolve datasource UID from -d flag, config, or Grafana auto-discovery.
			datasourceUID, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "mssql")
			if err != nil {
				return err
			}

			sql := mssql.EnforceTop(expr, limit, maxLimit)

			now := time.Now()
			start, end, _, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := mssql.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, mssql.QueryRequest{
				RawSQL: sql,
				Start:  start,
				End:    end,
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
		agent.AnnotationLLMHint:   `gcx datasources mssql query -d UID 'SELECT name, id FROM dbo.my_table' -o json`,
	}

	shared.Setup(cmd.Flags(), false)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.mssql is configured)")
	cmd.Flags().IntVar(&limit, "limit", defaultLimit, "Max rows to return via injected TOP (n) (0 disables injection)")
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
