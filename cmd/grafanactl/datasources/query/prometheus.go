package query

import (
	"fmt"
	"time"

	cmdconfig "github.com/grafana/grafanactl/cmd/grafanactl/config"
	"github.com/grafana/grafanactl/internal/query/prometheus"
	"github.com/spf13/cobra"
)

// PrometheusCmd returns the `query` subcommand for a Prometheus datasource parent.
func PrometheusCmd(configOpts *cmdconfig.Options) *cobra.Command {
	shared := &sharedQueryOpts{}

	cmd := &cobra.Command{
		Use:   "query [DATASOURCE_UID] EXPR",
		Short: "Execute a PromQL query against a Prometheus datasource",
		Long: `Execute a PromQL query against a Prometheus datasource.

DATASOURCE_UID is optional when datasources.prometheus is configured in your context.
EXPR is the PromQL expression to evaluate.`,
		Example: `
  # Instant query using configured default datasource
  grafanactl datasources prometheus query 'up{job="grafana"}'

  # Range query with explicit datasource UID
  grafanactl datasources prometheus query abc123 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m

  # Convenience window flag
  grafanactl datasources prometheus query abc123 'up' --window 1h

  # Output as JSON
  grafanactl datasources prometheus query abc123 'up' -o json`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			datasourceUID, expr, err := resolveTypedArgs(args, configOpts, ctx, "prometheus")
			if err != nil {
				return err
			}

			if err := validateDatasourceType(ctx, configOpts, datasourceUID, "prometheus"); err != nil {
				return err
			}

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			now := time.Now()
			start, end, step, err := shared.parseTimes(now)
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

			if shared.IO.OutputFormat == "table" {
				return prometheus.FormatTable(cmd.OutOrStdout(), resp)
			}

			return shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	shared.setup(cmd.Flags())

	return cmd
}
