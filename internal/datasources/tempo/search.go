package tempo

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

// QueryCmd returns the `query` subcommand for a Tempo datasource parent.
// It also registers `search` as a non-deprecated alias.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	share := &dsquery.ExploreLinkOpts{}
	var limit int
	var datasource string

	cmd := &cobra.Command{
		Use:     "query [TRACEQL]",
		Aliases: []string{"search"},
		Short:   "Search for traces using a TraceQL query",
		Long: `Search for traces using a TraceQL query against a Tempo datasource.

TRACEQL is the TraceQL expression to evaluate.
Datasource is resolved from -d flag or datasources.tempo in your context.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds. Share links require an
explicit time range via --since or --from/--to.`,
		Example: `
  # Search traces using configured default datasource
  gcx datasources tempo query '{ span.http.status_code >= 500 }'

  # Search with explicit datasource UID and time range
  gcx datasources tempo query -d UID '{ span.http.status_code >= 500 }' --since 1h

  # Print a Grafana Explore share link for the query
  gcx datasources tempo query '{ span.http.status_code >= 500 }' --share-link

  # With custom limit
  gcx datasources tempo query -d UID '{ span.http.status_code >= 500 }' --since 1h --limit 50

  # Output as JSON
  gcx datasources tempo query -d UID '{ span.http.status_code >= 500 }' -o json`,
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

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "tempo")
			if err != nil {
				return err
			}

			now := time.Now()
			start, end, _, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := tempo.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := tempo.SearchRequest{
				Query: expr,
				Start: start,
				End:   end,
				Limit: limit,
			}

			resp, err := client.Search(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}

			exploreURL := ""
			unavailableMsg, failedOpenMsg := dsquery.ExploreMessages("search")
			switch {
			case !shared.IsRange() && share.Enabled():
				unavailableMsg = "search succeeded, but Grafana Explore links require --since or --from/--to for Tempo trace searches"
			case limit == 0 && share.Enabled():
				unavailableMsg = "search succeeded, but Grafana Explore links do not support --limit 0 for Tempo trace searches"
			case shared.IsRange():
				exploreURL = SearchExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
					DatasourceUID:  datasourceUID,
					DatasourceType: "tempo",
					Expr:           expr,
					From:           shared.From,
					To:             shared.To,
					OrgID:          dsquery.OrgID(cfgCtx),
				}, limit)
			}

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
		agent.AnnotationLLMHint:   `gcx datasources tempo query -d UID '{ span.http.status_code >= 500 }' -o json`,
	}

	shared.Setup(cmd.Flags(), false)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of traces to return (0 means no limit)")
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
