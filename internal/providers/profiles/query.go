package profiles

import (
	"errors"
	"fmt"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
)

// queryCmd returns the `query` subcommand for a Pyroscope datasource parent.
func queryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var profileType string
	var maxNodes int64
	var datasource string

	cmd := &cobra.Command{
		Use:   "query EXPR",
		Short: "Execute a profiling query against a Pyroscope datasource",
		Long: `Execute a profiling query against a Pyroscope datasource.

EXPR is the label selector (e.g., '{service_name="frontend"}').
Datasource is resolved from -d flag or datasources.pyroscope in your context.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			if profileType == "" {
				return errors.New("--profile-type is required for pyroscope queries")
			}

			ctx := cmd.Context()

			// Resolve datasource UID from -d flag or config.
			var cfgCtx *internalconfig.Context
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err == nil {
				cfgCtx = fullCfg.GetCurrentContext()
			}

			datasourceUID, err := dsquery.ResolveDatasourceFlag(datasource, cfgCtx, "pyroscope")
			if err != nil {
				return err
			}

			expr := args[0]

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			dsType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			if err := dsquery.ValidateDatasourceType(dsType, "pyroscope"); err != nil {
				return err
			}

			now := time.Now()
			start, end, _, err := shared.ParseTimes(now)
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

	shared.Setup(cmd.Flags())
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
	cmd.Flags().StringVar(&profileType, "profile-type", "", "Profile type ID (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds') (required)")
	cmd.Flags().Int64Var(&maxNodes, "max-nodes", 1024, "Maximum nodes in flame graph")

	return cmd
}
