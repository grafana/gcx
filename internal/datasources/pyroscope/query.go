package pyroscope

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

const defaultMaxNodes int64 = 50000

// pprofCodec is a sentinel codec that registers "pprof" as a valid -o format.
// Actual pprof output is written to disk before Encode is ever reached.
type pprofCodec struct{}

func (c *pprofCodec) Format() format.Format { return "pprof" }
func (c *pprofCodec) Encode(_ io.Writer, _ any) error {
	return errors.New("pprof output is written to a file; use --pprof-path to specify the destination")
}
func (c *pprofCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("pprof codec does not support decoding")
}

// QueryCmd returns the `query` subcommand for a Pyroscope datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	// Register pprof before BindFlags so it appears in the -o help string.
	shared.IO.RegisterCustomCodec("pprof", &pprofCodec{})

	var profileType string
	var maxNodes int64
	var datasource string
	var profileIDs []string
	var stacktraceSelector []string
	var pprofPath string
	var pprofOverwrite bool

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

  # Drill into one or more specific profiles found via exemplars
  # (--profile-id is repeatable; pass it once per UUID)
  gcx datasources pyroscope query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h \
    --profile-id 550e8400-e29b-41d4-a716-446655440000 \
    --profile-id 7c9e6679-7425-40de-944b-e07fc1f90ae7

  # Restrict the flamegraph to stacks rooted at a specific call site
  # (--stacktrace-selector is repeatable; pass it once per frame, root first)
  gcx datasources pyroscope query '{service_name="my-go-service"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h \
    --stacktrace-selector 'github.com/prometheus/client_golang/prometheus.(*Registry).Gather.func1'

  # Download as pprof binary (for use with go tool pprof)
  gcx datasources pyroscope query -d UID '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds -o pprof

  # Download as pprof binary to a specific path
  gcx datasources pyroscope query -d UID '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds -o pprof --pprof-path ./cpu.pb.gz`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pprofFlagsChanged := cmd.Flags().Changed("pprof-path") || cmd.Flags().Changed("pprof-overwrite")
			if pprofFlagsChanged && shared.IO.OutputFormat != "pprof" {
				return errors.New("--pprof-path and --pprof-overwrite require -o pprof")
			}

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

			if shared.IO.OutputFormat == "pprof" {
				dest := pprofPath
				if dest == "" {
					dest = now.Format("profile-2006-01-02-150405.pb.gz")
				}
				if _, err := os.Stat(dest); err == nil && !pprofOverwrite {
					return fmt.Errorf("%s already exists; use --pprof-overwrite to overwrite", dest)
				}
				data, err := client.Pprof(ctx, datasourceUID, pyroscope.PprofRequest{
					LabelSelector: expr,
					ProfileTypeID: profileType,
					Start:         start,
					End:           end,
					MaxNodes:      maxNodes,
				})
				if err != nil {
					return fmt.Errorf("pprof fetch failed: %w", err)
				}
				if err := os.WriteFile(dest, data, 0o600); err != nil {
					return fmt.Errorf("writing pprof profile: %w", err)
				}
				result := &pyroscope.PprofWriteResult{Path: dest}
				return pyroscope.FormatPprofWriteTable(cmd.OutOrStdout(), result)
			}

			var sts *pyroscope.StackTraceSelector
			if len(stacktraceSelector) > 0 {
				locs := make([]pyroscope.Location, len(stacktraceSelector))
				for i, n := range stacktraceSelector {
					locs[i] = pyroscope.Location{Name: n}
				}
				sts = &pyroscope.StackTraceSelector{CallSite: locs}
			}

			resolvedMaxNodes := maxNodes
			if !cmd.Flags().Changed("max-nodes") {
				resolvedMaxNodes = defaultMaxNodes
			}
			req := pyroscope.QueryRequest{
				LabelSelector:      expr,
				ProfileTypeID:      profileType,
				Start:              start,
				End:                end,
				MaxNodes:           resolvedMaxNodes,
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
	cmd.Flags().StringVar(&profileType, "profile-type", "", "Profile type ID (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds'); use 'gcx profiles profile-types' to list available (required)")
	cmd.Flags().Int64Var(&maxNodes, "max-nodes", 0, fmt.Sprintf("Maximum nodes in flame graph (default 0/unlimited for pprof output, %d for all other formats)", defaultMaxNodes))
	cmd.Flags().StringSliceVar(&profileIDs, "profile-id", nil, "Drill down to specific profile UUIDs from exemplar queries (repeatable)")
	cmd.Flags().StringSliceVar(&stacktraceSelector, "stacktrace-selector", nil, "Only query locations with these function names, starting from the root (repeatable)")
	cmd.Flags().StringVar(&pprofPath, "pprof-path", "", "Destination path for pprof binary output (only with -o pprof; default: profile-YYYY-MM-DD-HHMMSS.pb.gz)")
	cmd.Flags().BoolVar(&pprofOverwrite, "pprof-overwrite", false, "Overwrite the output file if it already exists (only with -o pprof)")

	return cmd
}
