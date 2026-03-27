package query

import (
	"errors"
	"fmt"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
)

// PyroscopeCmd returns the `query` subcommand for a Pyroscope datasource parent.
func PyroscopeCmd(configOpts *cmdconfig.Options) *cobra.Command {
	shared := &sharedQueryOpts{}
	var profileType string
	var maxNodes int64

	cmd := &cobra.Command{
		Use:   "query [DATASOURCE_UID] EXPR",
		Short: "Execute a profiling query against a Pyroscope datasource",
		Long: `Execute a profiling query against a Pyroscope datasource.

DATASOURCE_UID is optional when datasources.pyroscope is configured in your context.
EXPR is the label selector (e.g., '{service_name="frontend"}').`,
		Example: `
  # Profile query with explicit datasource UID
  gcx datasources pyroscope query pyro-001 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
    --from now-1h --to now

  # Using configured default datasource
  gcx datasources pyroscope query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
    --window 1h

  # Output as JSON
  gcx datasources pyroscope query pyro-001 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds -o json`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			if profileType == "" {
				return errors.New("--profile-type is required for pyroscope queries")
			}

			ctx := cmd.Context()

			datasourceUID, expr, err := resolveTypedArgs(args, configOpts, ctx, "pyroscope")
			if err != nil {
				return err
			}

			if err := validateDatasourceType(ctx, configOpts, datasourceUID, "pyroscope"); err != nil {
				return err
			}

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			now := time.Now()
			start, end, _, err := shared.parseTimes(now)
			if err != nil {
				return err
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
		},
	}

	shared.setup(cmd.Flags())
	cmd.Flags().StringVar(&profileType, "profile-type", "", "Profile type ID (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds') (required)")
	cmd.Flags().Int64Var(&maxNodes, "max-nodes", 1024, "Maximum nodes in flame graph")

	return cmd
}
