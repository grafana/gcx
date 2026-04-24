package prometheus

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/grafana-app-sdk/logging"
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
	var datasource string

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a PromQL query against a Prometheus datasource",
		Long: `Execute a PromQL query against a Prometheus datasource.

EXPR is the PromQL expression to evaluate, passed as a positional argument or
via --expr (familiar to promtool users).
Datasource is resolved from -d flag or datasources.prometheus in your context.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		Example: `
  # Instant query using configured default datasource
  gcx datasources prometheus query 'up{job="grafana"}'

  # Range query with explicit datasource UID
  gcx datasources prometheus query -d UID 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m

  # Query the last hour
  gcx datasources prometheus query 'up' --since 1h

  # Print a Grafana Explore share link for the executed query
  gcx datasources prometheus query 'up' --share-link

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

			effectiveDS := datasource
			if effectiveDS == "" {
				effectiveDS = defaultDS
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, effectiveDS, cfgCtx, cfg, "prometheus")
			if err != nil {
				return err
			}

			dsType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			if err := dsquery.ValidateDatasourceType(dsType, "prometheus"); err != nil {
				return err
			}

			now := time.Now()
			start, end, step, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := prometheus.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := prometheus.QueryRequest{
				Query: expr,
				Start: start,
				End:   end,
				Step:  step,
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			exploreURL := ExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
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
		agent.AnnotationLLMHint:   `gcx datasources prometheus query -d UID 'up{job="grafana"}' -o json`,
	}

	shared.Setup(cmd.Flags(), true)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.prometheus is configured)")
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
