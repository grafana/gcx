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

Default --limit is 50; use --limit 0 for no cap.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		Example: `
  # Query logs using configured default datasource
  gcx datasources loki query '{job="varlogs"}'

  # Query with explicit datasource UID
  gcx datasources loki query -d UID '{job="varlogs"} |= "error"'

  # Print a Grafana Explore share link for the query
  gcx datasources loki query '{job="varlogs"}' --share-link

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
				Limit: limit,
			}

			resp, err := client.Query(ctx, datasourceUID, req)
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
		agent.AnnotationLLMHint:   `gcx datasources loki query -d UID '{job="grafana"}' -o json`,
	}

	dsquery.RegisterCodecs(&shared.IO, false)
	shared.IO.RegisterCustomCodec("raw", loki.NewRawQueryCodec())
	shared.IO.BindFlags(cmd.Flags())
	shared.SetupTimeFlags(cmd.Flags())
	cmd.Flags().StringVar(&shared.Step, "step", "", "Query step (e.g., '15s', '1m')")
	shared.SetupExprFlag(cmd.Flags())
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.loki is configured)")
	cmd.Flags().IntVar(&limit, "limit", dsquery.DefaultLokiLimit, "Maximum number of log lines to return (0 means no limit)")
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
