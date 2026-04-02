package traces

import (
	"errors"
	"fmt"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/spf13/cobra"
)

// metricsCmd returns the `metrics` subcommand for TraceQL metrics queries.
func metricsCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var instant bool

	cmd := &cobra.Command{
		Use:   "metrics [DATASOURCE_UID] TRACEQL",
		Short: "Execute a TraceQL metrics query",
		Long: `Execute a TraceQL metrics query against a Tempo datasource.

DATASOURCE_UID is optional when datasources.tempo is configured in your context.
TRACEQL is the TraceQL metrics expression to evaluate.

By default, this runs a range query. Use --instant for point-in-time queries.`,
		Example: `
  # Range query using configured default datasource
  gcx traces metrics '{ } | rate()'

  # Range query with explicit datasource and time range
  gcx traces metrics tempo-001 '{ } | rate()' --since 1h

  # Instant query
  gcx traces metrics tempo-001 '{ } | rate()' --instant --since 1h

  # Custom step interval
  gcx traces metrics tempo-001 '{ } | rate()' --since 1h --step 30s

  # Output as JSON
  gcx traces metrics tempo-001 '{ } | rate()' -o json`,
		Args: validateMetricsArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Resolve default UID from config.
			var defaultUID string
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err == nil {
				defaultUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "tempo")
			}

			datasourceUID, expr, err := dsquery.ResolveTypedArgs(args, defaultUID, "tempo")
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

			step := shared.Step
			if step == "" && !instant {
				step = "60s"
			}

			req := tempo.MetricsRequest{
				Query:   expr,
				Start:   start,
				End:     end,
				Step:    step,
				Instant: instant,
			}

			var resp *tempo.MetricsResponse
			if instant {
				resp, err = client.MetricsInstant(ctx, datasourceUID, req)
			} else {
				resp, err = client.MetricsRange(ctx, datasourceUID, req)
			}
			if err != nil {
				return fmt.Errorf("metrics query failed: %w", err)
			}

			switch shared.IO.OutputFormat {
			case "table":
				return tempo.FormatMetricsTable(cmd.OutOrStdout(), resp)
			default:
				return shared.IO.Encode(cmd.OutOrStdout(), resp)
			}
		},
	}

	shared.Setup(cmd.Flags())
	cmd.Flags().BoolVar(&instant, "instant", false, "Execute an instant query instead of a range query")

	return cmd
}

func validateMetricsArgs(_ *cobra.Command, args []string) error {
	switch len(args) {
	case 0:
		return errors.New("TRACEQL is required")
	case 1, 2:
		return nil
	default:
		return errors.New("too many arguments: expected [DATASOURCE_UID] TRACEQL")
	}
}
