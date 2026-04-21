package influxdb

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

// QueryCmd returns the `query` subcommand for an InfluxDB datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var datasource string

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a query against an InfluxDB datasource",
		Long: `Execute a query against an InfluxDB datasource.

EXPR is the InfluxQL, Flux, or SQL expression to evaluate, passed as a positional
argument or via --expr.
The query language mode is auto-detected from the datasource configuration.
Datasource is resolved from -d flag or datasources.influxdb in your context.`,
		Example: `
  # InfluxQL instant query
  gcx datasources influxdb query -d UID 'SELECT mean("value") FROM "cpu" WHERE time > now() - 1h'

  # InfluxQL range query
  gcx datasources influxdb query -d UID 'SELECT mean("value") FROM "cpu"' --from now-1h --to now

  # Output as JSON
  gcx datasources influxdb query -d UID 'SELECT * FROM "cpu" LIMIT 10' -o json`,
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

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "influxdb")
			if err != nil {
				return err
			}

			dsType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			if err := dsquery.ValidateDatasourceType(dsType, "influxdb"); err != nil {
				return err
			}

			modeStr, err := dsquery.GetInfluxDBMode(ctx, cfg, datasourceUID)
			if err != nil {
				return fmt.Errorf("failed to detect influxdb mode: %w", err)
			}

			now := time.Now()
			start, end, _, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := influxdb.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := influxdb.QueryRequest{
				Query: expr,
				Start: start,
				End:   end,
				Mode:  influxdb.Mode(modeStr),
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			return shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources influxdb query -d UID 'SELECT mean("value") FROM "cpu" WHERE time > now() - 1h' -o json`,
	}

	shared.Setup(cmd.Flags(), false)
	_ = cmd.Flags().MarkHidden("step")
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.influxdb is configured)")

	return cmd
}
