package athena

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/athena"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

// QueryCmd returns the `query` subcommand for an Athena datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	share := &dsquery.ExploreLinkOpts{}
	var datasource string
	var limit int
	var region, catalog, database string
	var resultReuse bool
	var ttlMinutes int

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a SQL query against an Athena datasource",
		Long: `Execute a SQL query against an Amazon Athena datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.athena in your context.
Server-side macros ($__timeFilter, $__dateFilter, etc.) are supported.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		Example: `
  # Simple query
  gcx datasources athena query 'SELECT count(*) FROM events'

  # With time macro and explicit datasource
  gcx datasources athena query -d UID 'SELECT * FROM logs WHERE $__timeFilter(event_time)' --since 1h

  # With connection overrides
  gcx datasources athena query -d UID 'SELECT 1' --region us-west-2 --database analytics

  # Enable result reuse (Athena engine v3)
  gcx datasources athena query -d UID 'SELECT count(*) FROM events' --result-reuse --ttl-minutes 60

  # Disable limit enforcement
  gcx datasources athena query 'SELECT * FROM big_table' --limit 0`,
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

			// Resolve datasource UID from -d flag, config, or Grafana auto-discovery.
			datasourceUID, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "athena")
			if err != nil {
				return err
			}

			sql := athena.EnforceLimit(expr, limit, maxLimit)

			now := time.Now()
			start, end, _, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := athena.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, athena.QueryRequest{
				RawSQL:                     sql,
				Start:                      start,
				End:                        end,
				Region:                     region,
				Catalog:                    catalog,
				Database:                   database,
				ResultReuseEnabled:         resultReuse,
				ResultReuseMaxAgeInMinutes: ttlMinutes,
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
		agent.AnnotationLLMHint:   `gcx datasources athena query -d UID 'SELECT count(*) FROM events' -o json`,
	}

	shared.Setup(cmd.Flags(), false)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.athena is configured)")
	cmd.Flags().IntVar(&limit, "limit", defaultLimit, "Max rows to return (0 disables enforcement)")
	cmd.Flags().StringVar(&region, "region", "", "AWS region override")
	cmd.Flags().StringVar(&catalog, "catalog", "", "Data catalog override")
	cmd.Flags().StringVar(&database, "database", "", "Database override")
	cmd.Flags().BoolVar(&resultReuse, "result-reuse", false, "Enable Athena query result reuse (engine v3)")
	cmd.Flags().IntVar(&ttlMinutes, "ttl-minutes", 60, "Cache TTL in minutes for result reuse; 0 disables")
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
