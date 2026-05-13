package pyroscope

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

// QueryCmd returns the `query` subcommand for a Pyroscope datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var profileType string
	var maxNodes int64
	var datasource string
	var profileIDs []string
	var stacktraceSelector []string

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a profiling query against a Pyroscope datasource",
		Long: `Execute a profiling query against a Pyroscope datasource.

EXPR is the label selector (e.g., '{service_name="frontend"}').
Datasource is resolved from -d flag or datasources.pyroscope in your context.`,
		Example: `
  # Profile query with explicit datasource UID
  gcx datasources pyroscope query -d UID '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Using configured default datasource
  gcx datasources pyroscope query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Output as JSON
  gcx datasources pyroscope query -d UID '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds -o json

  # Drill into a specific profile found via exemplars
  gcx datasources pyroscope query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
    --since 1h --profile-id <profile-uuid>`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			if profileType == "" {
				return errors.New("--profile-type is required for pyroscope queries")
			}

			for _, id := range profileIDs {
				if _, err := uuid.Parse(id); err != nil {
					return fmt.Errorf("--profile-id must be a valid UUID (got %q)", id)
				}
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

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "pyroscope")
			if err != nil {
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

			var sts *pyroscope.StackTraceSelector
			if len(stacktraceSelector) > 0 {
				locs := make([]pyroscope.Location, len(stacktraceSelector))
				for i, n := range stacktraceSelector {
					locs[i] = pyroscope.Location{Name: n}
				}
				sts = &pyroscope.StackTraceSelector{CallSite: locs}
			}

			req := pyroscope.QueryRequest{
				LabelSelector:      expr,
				ProfileTypeID:      profileType,
				Start:              start,
				End:                end,
				MaxNodes:           maxNodes,
				ProfileIDs:         profileIDs,
				StackTraceSelector: sts,
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

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources pyroscope query -d UID '{service_name="frontend"}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h -o json`,
	}

	shared.Setup(cmd.Flags(), true)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
	cmd.Flags().StringVar(&profileType, "profile-type", "", "Profile type ID (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds') (required)")
	cmd.Flags().Int64Var(&maxNodes, "max-nodes", 1024, "Maximum nodes in flame graph")
	cmd.Flags().StringSliceVar(&profileIDs, "profile-id", nil, "Drill down to specific profile UUIDs from exemplar queries (repeatable)")
	cmd.Flags().StringSliceVar(&stacktraceSelector, "stacktrace-selector", nil, "Only query locations with these function names, starting from the root (repeatable)")

	return cmd
}
