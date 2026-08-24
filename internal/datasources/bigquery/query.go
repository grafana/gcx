package bigquery

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/bigquery"
	"github.com/spf13/cobra"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

// QueryCmd returns the `query` subcommand for a BigQuery datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	share := &dsquery.ExploreLinkOpts{}
	var datasource string
	var limit int

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a SQL query against a BigQuery datasource",
		Long: `Execute a GoogleSQL query against a BigQuery datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.bigquery in your context.
Reference tables as ` + "`project.dataset.table`" + `; when the project is omitted
the datasource's default project is used.
Server-side macros ($__timeFilter, etc.) are supported against TIMESTAMP
columns; a DATETIME column needs an explicit CAST(col AS TIMESTAMP) first,
since $__timeFilter substitutes a TIMESTAMP literal BigQuery won't compare
against DATETIME directly.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		Example: `
  # Simple query
  gcx datasources bigquery query 'SELECT count(*) FROM ` + "`my-project.my_dataset.events`" + `'

  # Explicit datasource
  gcx datasources bigquery query -d UID 'SELECT * FROM ` + "`my_dataset.logs`" + `' --since 1h

  # $__timeFilter against a TIMESTAMP column
  gcx datasources bigquery query -d UID 'SELECT * FROM ` + "`my_dataset.events`" + ` WHERE $__timeFilter(event_ts)' --since 1h

  # Output as JSON
  gcx datasources bigquery query -d UID 'SELECT 1' -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources bigquery query 'SELECT 1' --share-link

  # Disable limit enforcement
  gcx datasources bigquery query 'SELECT * FROM ` + "`my_dataset.big_table`" + `' --limit 0`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}
			if limit < 0 {
				return fmt.Errorf("--limit must be >= 0, got %d", limit)
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

			datasourceUID, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "bigquery")
			if err != nil {
				return err
			}

			sql, capped := bigquery.EnforceLimit(expr, limit, maxLimit)
			if capped {
				cmdio.Warning(cmd.ErrOrStderr(), "LIMIT in query exceeds the maximum of %d and was capped; use --limit 0 to disable enforcement", maxLimit)
			}

			now := time.Now()
			start, end, _, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := bigquery.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, bigquery.QueryRequest{
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
		agent.AnnotationLLMHint:   "gcx datasources bigquery query -d UID 'SELECT count(*) FROM `dataset.events`' -o json",
	}

	shared.Setup(cmd.Flags(), false)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.bigquery is configured)")
	cmd.Flags().IntVar(&limit, "limit", defaultLimit, fmt.Sprintf("Max rows to return; requests above %d are capped, with a warning (0 disables enforcement)", maxLimit))
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
