package pinot

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pinot"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

type queryOpts struct {
	dsquery.SharedOpts

	Datasource string
	Limit      int
}

func (opts *queryOpts) setup(flags *pflag.FlagSet) {
	opts.Setup(flags, false)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pinot is configured)")
	flags.IntVar(&opts.Limit, "limit", defaultLimit, fmt.Sprintf("Max rows to return; requests above %d are capped, with a warning (0 disables enforcement)", maxLimit))
}

func (opts *queryOpts) Validate() error {
	if opts.Limit < 0 {
		return fmt.Errorf("--limit must be >= 0, got %d", opts.Limit)
	}
	return opts.SharedOpts.Validate()
}

// QueryCmd returns the `query` subcommand for a Pinot datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &queryOpts{}
	share := &dsquery.ExploreLinkOpts{}

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a PinotQL query against a StarTree Pinot datasource",
		Long: `Execute a PinotQL query against a StarTree Pinot datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.pinot in your context.
Server-side macros ($__timeFilter, $__timeGroup, etc.) are supported.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		Example: `
  # Simple query
  gcx datasources pinot query -d UID 'SELECT count(*) FROM faro_pinot_events_v2'

  # With time range
  gcx datasources pinot query -d UID --since 7d \
    'SELECT count(*) FROM faro_pinot_events_v2 WHERE appId = 66'

  # Output as JSON
  gcx datasources pinot query -d UID 'SELECT 1 FROM faro_pinot_events_v2' -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources pinot query -d UID 'SELECT 1 FROM faro_pinot_events_v2' --share-link

  # Disable limit enforcement
  gcx datasources pinot query -d UID 'SELECT * FROM faro_pinot_events_v2' --limit 0`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			expr, err := opts.ResolveExpr(args, 0)
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "pinot")
			if err != nil {
				return err
			}

			sql, capped := pinot.EnforceLimit(expr, opts.Limit, maxLimit)
			if capped {
				cmdio.Warning(cmd.ErrOrStderr(), "LIMIT in query exceeds the maximum of %d and was capped; use --limit 0 to disable enforcement", maxLimit)
			}

			now := time.Now()
			start, end, step, err := opts.ParseTimes(now)
			if err != nil {
				return err
			}

			var intervalMs int64
			if step > 0 {
				intervalMs = step.Milliseconds()
			}

			client, err := pinot.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, pinot.QueryRequest{
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
				From:           opts.From,
				To:             opts.To,
				OrgID:          dsquery.OrgID(cfgCtx),
			}, pinot.ExtractTableName(sql))
			unavailableMsg, failedOpenMsg := dsquery.ExploreMessages("query")

			return dsquery.EncodeAndHandleExplore(cmd, func() error {
				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}, *share, dsquery.ExploreLink{
				URL:            exploreURL,
				UnavailableMsg: unavailableMsg,
				FailedOpenMsg:  failedOpenMsg,
			})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources pinot query -d UID 'SELECT count(*) FROM faro_pinot_events_v2' -o json`,
	}

	opts.setup(cmd.Flags())
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
