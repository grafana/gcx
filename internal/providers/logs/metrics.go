package logs

import (
	"fmt"
	"log/slog"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

// metricsCmd returns the `metrics` subcommand for metric LogQL queries.
func metricsCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var datasource string

	cmd := &cobra.Command{
		Use:   "metrics EXPR",
		Short: "Execute a metric LogQL query against a Loki datasource",
		Long: `Execute a metric LogQL query and return time-series results.

EXPR is a metric LogQL expression (e.g., rate, count_over_time, sum).
Datasource is resolved from -d flag or datasources.loki in your context.

Unlike 'logs query' which returns log lines, 'logs metrics' returns
time-series data with proper table, graph, and JSON formatters.

Instant vs range is deduced from time flags: no time flags = instant query,
--since or --from/--to = range query.`,
		Example: `
  # Rate of log lines over 5 minutes
  gcx logs metrics 'rate({job="varlogs"}[5m])' --since 1h -o table

  # Count of error logs
  gcx logs metrics 'count_over_time({job="varlogs"} |= "error" [5m])' --since 1h

  # Line chart output
  gcx logs metrics -d loki-001 'rate({job="varlogs"}[5m])' --since 1h -o graph

  # Output as JSON
  gcx logs metrics 'rate({job="varlogs"}[5m])' --since 1h -o json`,
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

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "loki")
			if err != nil {
				return err
			}

			expr := args[0]

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
			}

			resp, err := client.MetricQuery(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("metric query failed: %w", err)
			}

			return shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	shared.Setup(cmd.Flags(), true)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.loki is configured)")

	return cmd
}
