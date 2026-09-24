package loki

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/spf13/cobra"
)

// QueryCmd returns the `query` subcommand for a Loki datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	share := &dsquery.ExploreLinkOpts{}
	preflight := &statsPreflightOpts{}
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
open it in your browser after the query succeeds.

Before executing, a pre-flight index-stats check estimates the bytes this
query would scan and prints a non-blocking warning if it exceeds
--stats-warn-bytes (default 10GiB). Set --stats-max-bytes to refuse to run the
query at all above that many bytes — this is blocking, so unlike the default
warn-only check it does add the pre-flight call's latency to the command.
Use --skip-stats to disable both checks entirely.
Only the query's stream selector is used for the estimate, since Loki's index
tracks streams, not line filters or parsing stages. The checked window is
widened by any range-vector duration or offset in EXPR (e.g. '[24h]',
'offset 1h'), since Loki evaluates further back than the query's own time
range alone would suggest.`,
		Example: `
  # Query logs using configured default datasource
  gcx datasources loki query '{job="varlogs"}'

  # Query with explicit datasource UID
  gcx datasources loki query -d UID '{job="varlogs"} |= "error"'

  # Print a Grafana Explore share link for the query
  gcx datasources loki query '{job="varlogs"}' --share-link

  # Refuse to run if the query would scan more than 5GiB
  gcx datasources loki query '{job="varlogs"}' --stats-max-bytes 5GiB

  # Raw line bodies only
  gcx datasources loki query -d UID '{job="varlogs"}' -o raw

  # Output as JSON
  gcx datasources loki query -d UID '{job="varlogs"}' -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}
			if err := preflight.Validate(); err != nil {
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

			wait, cancelPreflight, err := startStatsPreflight(ctx, client, cmd.ErrOrStderr(), datasourceUID, expr, req.IsRange(), start, end, now, preflight)
			if err != nil {
				return err
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			// Grace window avoids losing the check to a fast query (see statsPreflightGraceAfterQuery).
			finishStatsPreflight(wait, cancelPreflight)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
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

	dsquery.RegisterCodecs(&shared.IO, false)
	shared.IO.RegisterCustomCodec("raw", loki.NewRawQueryCodec())
	shared.IO.BindFlags(cmd.Flags())
	shared.SetupTimeFlags(cmd.Flags())
	shared.SetupErrorOnEmptyFlag(cmd.Flags())
	cmd.Flags().StringVar(&shared.Step, "step", "", "Query step (e.g., '15s', '1m')")
	shared.SetupExprFlag(cmd.Flags())
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.loki is configured)")
	cmd.Flags().IntVar(&limit, "limit", dsquery.DefaultLokiLimit, "Maximum number of log lines to return (0 means no limit)")
	share.Setup(cmd.Flags(), "executed query")
	preflight.setup(cmd.Flags())

	return cmd
}
