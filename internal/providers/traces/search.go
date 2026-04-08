package traces

import (
	"fmt"
	"log/slog"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

// queryCmd returns the `query` subcommand for a Tempo datasource parent.
// It also registers `search` as a non-deprecated alias.
func queryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var limit int
	var datasource string

	cmd := &cobra.Command{
		Use:     "query TRACEQL",
		Aliases: []string{"search"},
		Short:   "Search for traces using a TraceQL query",
		Long: `Search for traces using a TraceQL query against a Tempo datasource.

TRACEQL is the TraceQL expression to evaluate.
Datasource is resolved from -d flag or datasources.tempo in your context.`,
		Example: `
  # Search traces using configured default datasource
  gcx traces query '{ span.http.status_code >= 500 }'

  # Search with explicit datasource UID and time range
  gcx traces query -d tempo-001 '{ span.http.status_code >= 500 }' --since 1h

  # Using the search alias
  gcx traces search '{ span.http.status_code >= 500 }' --since 1h

  # With custom limit
  gcx traces query -d tempo-001 '{ span.http.status_code >= 500 }' --since 1h --limit 50

  # Output as JSON
  gcx traces query -d tempo-001 '{ span.http.status_code >= 500 }' -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
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

			expr := args[0]

			dsType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			if err := dsquery.ValidateDatasourceType(dsType, "tempo"); err != nil {
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

			return shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	shared.Setup(cmd.Flags(), false)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of traces to return (0 means no limit)")

	return cmd
}
