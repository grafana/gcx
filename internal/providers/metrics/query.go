package metrics

import (
	"fmt"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/spf13/cobra"
)

// queryCmd returns the `query` subcommand for a Prometheus datasource parent.
func queryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}

	cmd := &cobra.Command{
		Use:   "query [DATASOURCE_UID] EXPR",
		Short: "Execute a PromQL query against a Prometheus datasource",
		Long: `Execute a PromQL query against a Prometheus datasource.

DATASOURCE_UID is optional when datasources.prometheus is configured in your context.
EXPR is the PromQL expression to evaluate.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Resolve default UID from config.
			var defaultUID string
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err == nil {
				defaultUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "prometheus")
			}

			datasourceUID, expr, err := dsquery.ResolveTypedArgs(args, defaultUID, "prometheus")
			if err != nil {
				return err
			}

			cfg, err := loader.LoadGrafanaConfig(ctx)
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

			if shared.IO.OutputFormat == "table" {
				return prometheus.FormatTable(cmd.OutOrStdout(), resp)
			}

			return shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	shared.Setup(cmd.Flags())

	return cmd
}
