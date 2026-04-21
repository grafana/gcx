package datasources

import (
	"errors"
	"fmt"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
)

// QueryCmd returns the auto-detecting query command for the datasources group.
func QueryCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	shared := &dsquery.SharedOpts{}
	var profileType string
	var maxNodes int64
	var limit int

	cmd := &cobra.Command{
		Use:   "query DATASOURCE_UID [EXPR]",
		Short: "Execute a query against any datasource (auto-detects type)",
		Long: `Execute a query against any datasource, automatically detecting the datasource type.

DATASOURCE_UID is always required (no default resolution for generic).
EXPR is the query expression appropriate for the datasource type.

The datasource type is detected via the Grafana API and the appropriate query
client is used automatically. This is the escape hatch for datasource types
that do not have a dedicated subcommand.`,
		Example: `
  # Auto-detect and query any supported datasource
  gcx datasources query ds-001 'up{job="grafana"}' --from now-1h --to now

  # Loki via auto-detect (with limit)
  gcx datasources query loki-001 '{job="varlogs"}' --from now-1h --to now --limit 200

  # Pyroscope via auto-detect
  gcx datasources query pyro-001 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --from now-1h --to now`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			datasourceUID := args[0]
			expr, err := shared.ResolveExpr(args, 1)
			if err != nil {
				return err
			}

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			rawType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			dsType := dsquery.NormalizeKind(rawType)

			now := time.Now()
			start, end, step, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			switch dsType {
			case "prometheus":
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

				if shared.IO.OutputFormat == "table" {
					return prometheus.FormatTable(cmd.OutOrStdout(), resp)
				}

				return shared.IO.Encode(cmd.OutOrStdout(), resp)

			case "loki":
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

				switch shared.IO.OutputFormat {
				case "table":
					return loki.FormatQueryTable(cmd.OutOrStdout(), resp)
				case "wide":
					return loki.FormatQueryTableWide(cmd.OutOrStdout(), resp)
				default:
					return shared.IO.Encode(cmd.OutOrStdout(), resp)
				}

			case "pyroscope":
				if profileType == "" {
					return errors.New("--profile-type is required for pyroscope queries")
				}

				client, err := pyroscope.NewClient(cfg)
				if err != nil {
					return fmt.Errorf("failed to create client: %w", err)
				}

				req := pyroscope.QueryRequest{
					LabelSelector: expr,
					ProfileTypeID: profileType,
					Start:         start,
					End:           end,
					MaxNodes:      maxNodes,
				}

				resp, err := client.Query(ctx, datasourceUID, req)
				if err != nil {
					return fmt.Errorf("query failed: %w", err)
				}

				if shared.IO.OutputFormat == "table" {
					return pyroscope.FormatQueryTable(cmd.OutOrStdout(), resp)
				}

				return shared.IO.Encode(cmd.OutOrStdout(), resp)

			case "influxdb":
				influxClient, err := influxdb.NewClient(cfg)
				if err != nil {
					return fmt.Errorf("failed to create client: %w", err)
				}

				modeStr, err := dsquery.GetInfluxDBMode(ctx, cfg, datasourceUID)
				if err != nil {
					return fmt.Errorf("failed to detect influxdb mode: %w", err)
				}

				req := influxdb.QueryRequest{
					Query: expr,
					Start: start,
					End:   end,
					Step:  step,
					Mode:  influxdb.Mode(modeStr),
				}

				resp, err := influxClient.Query(ctx, datasourceUID, req)
				if err != nil {
					return fmt.Errorf("query failed: %w", err)
				}

				return shared.IO.Encode(cmd.OutOrStdout(), resp)

			default:
				return fmt.Errorf("datasource type %q is not supported (supported: prometheus, loki, pyroscope, influxdb)", dsType)
			}
		},
	}

	configOpts.BindFlags(cmd.Flags())
	shared.Setup(cmd.Flags(), true)
	cmd.Flags().StringVar(&profileType, "profile-type", "", "Profile type ID for pyroscope queries (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds')")
	cmd.Flags().Int64Var(&maxNodes, "max-nodes", 1024, "Maximum nodes in flame graph (pyroscope only)")
	cmd.Flags().IntVar(&limit, "limit", dsquery.DefaultLokiLimit, "Maximum number of log lines to return for loki queries (0 means no limit)")

	return cmd
}
