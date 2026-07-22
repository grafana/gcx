package pyroscope

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

type pyroscopeQueryOpts struct {
	shared             dsquery.SharedOpts
	Datasource         string
	ProfileType        string
	MaxNodes           int64
	ProfileIDs         []string
	SpanIDs            []string
	TraceIDs           []string
	StacktraceSelector []string
	PprofPath          string
	PprofOverwrite     bool
}

func (opts *pyroscopeQueryOpts) setup(flags *pflag.FlagSet) {
	// Register pprof before shared.Setup so it appears in the -o help string.
	opts.shared.IO.RegisterCustomCodec("pprof", &pprofCodec{})
	opts.shared.Setup(flags, true)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
	flags.StringVar(&opts.ProfileType, "profile-type", "", "Profile type ID (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds'); use 'gcx profiles list-profile-types' to list available (required)")
	flags.Int64Var(&opts.MaxNodes, "max-nodes", 0, fmt.Sprintf("Maximum nodes in flame graph (default 0/unlimited for pprof output, %d for all other formats)", defaultMaxNodes))
	flags.StringSliceVar(&opts.ProfileIDs, "profile-id", nil, "Drill down to specific profile UUIDs from exemplar queries (repeatable)")
	flags.StringSliceVar(&opts.SpanIDs, "span-id", nil, "Only query profiles with these 16-character hex span IDs (repeatable; unavailable with -o pprof)")
	flags.StringSliceVar(&opts.TraceIDs, "trace-id", nil, "Only query samples with these 32-character hex trace IDs (repeatable)")
	flags.StringSliceVar(&opts.StacktraceSelector, "stacktrace-selector", nil, "Only query locations with these function names, starting from the root (repeatable)")
	flags.StringVar(&opts.PprofPath, "pprof-path", "", "Destination path for pprof binary output (only with -o pprof; default: profile-YYYY-MM-DD-HHMMSS.pb.gz)")
	flags.BoolVar(&opts.PprofOverwrite, "pprof-overwrite", false, "Overwrite the output file if it already exists (only with -o pprof)")
}

func (opts *pyroscopeQueryOpts) Validate(flags *pflag.FlagSet) error {
	if flags.Changed("pprof-path") || flags.Changed("pprof-overwrite") {
		if opts.shared.IO.OutputFormat != "pprof" {
			return errors.New("--pprof-path and --pprof-overwrite require -o pprof")
		}
	}
	if err := opts.shared.Validate(); err != nil {
		return err
	}
	if opts.ProfileType == "" {
		return errors.New("--profile-type is required for pyroscope queries")
	}
	for _, id := range opts.ProfileIDs {
		if !isUUID(id) {
			return fmt.Errorf("--profile-id must be a valid UUID (got %q)", id)
		}
	}
	if len(opts.SpanIDs) > 0 && len(opts.StacktraceSelector) > 0 {
		return errors.New("--span-id and --stacktrace-selector cannot be used together")
	}
	if len(opts.SpanIDs) > 0 && len(opts.ProfileIDs) > 0 {
		return errors.New("--span-id and --profile-id cannot be used together")
	}
	if len(opts.TraceIDs) > 0 && len(opts.SpanIDs) > 0 {
		return errors.New("--trace-id and --span-id cannot be used together")
	}
	if len(opts.TraceIDs) > 0 && len(opts.ProfileIDs) > 0 {
		return errors.New("--trace-id and --profile-id cannot be used together")
	}
	if len(opts.SpanIDs) > 0 && opts.shared.IO.OutputFormat == "pprof" {
		return errors.New("--span-id is not supported with -o pprof")
	}
	for _, id := range opts.SpanIDs {
		if !isHexID(id, 16) {
			return fmt.Errorf("--span-id must be a 16-character hex span ID (got %q)", id)
		}
	}
	for _, id := range opts.TraceIDs {
		if !isHexID(id, 32) {
			return fmt.Errorf("--trace-id must be a 32-character hex trace ID (got %q)", id)
		}
	}
	return nil
}

// stackTraceSelector builds the StackTraceSelector message from the
// --stacktrace-selector flag values. Returns nil when no values are set.
func (opts *pyroscopeQueryOpts) stackTraceSelector() *pyroscope.StackTraceSelector {
	if len(opts.StacktraceSelector) == 0 {
		return nil
	}
	locs := make([]pyroscope.Location, len(opts.StacktraceSelector))
	for i, n := range opts.StacktraceSelector {
		locs[i] = pyroscope.Location{Name: n}
	}
	return &pyroscope.StackTraceSelector{CallSite: locs}
}

// resolveMaxNodes returns the effective MaxNodes for non-pprof formats.
// pprof output is left at MaxNodes=0 (server default / unlimited).
func (opts *pyroscopeQueryOpts) resolveMaxNodes(flags *pflag.FlagSet) int64 {
	if flags.Changed("max-nodes") {
		return opts.MaxNodes
	}
	return defaultMaxNodes
}

// QueryCmd returns the `query` subcommand for a Pyroscope datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pyroscopeQueryOpts{}

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

  # Restrict the query to one or more trace spans
  gcx datasources pyroscope query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h \
    --span-id 00f067aa0ba902b7

  # Restrict the query to samples from one or more traces
  gcx datasources pyroscope query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h \
    --trace-id 4bf92f3577b34da6a3ce929d0e0e4736

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
			if err := opts.Validate(cmd.Flags()); err != nil {
				return err
			}

			expr, err := opts.shared.ResolveExpr(args, 0)
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			// Resolve datasource UID from -d flag, config, or Grafana auto-discovery.
			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "pyroscope")
			if err != nil {
				return err
			}

			now := time.Now()
			start, end, _, err := opts.shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := pyroscope.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if opts.shared.IO.OutputFormat == "pprof" {
				dest := opts.PprofPath
				if dest == "" {
					dest = now.Format("profile-2006-01-02-150405.pb.gz")
				}
				if _, err := os.Stat(dest); err == nil && !opts.PprofOverwrite {
					return fmt.Errorf("%s already exists; use --pprof-overwrite to overwrite", dest)
				}
				data, err := client.Pprof(ctx, datasourceUID, pyroscope.PprofRequest{
					LabelSelector: expr,
					ProfileTypeID: opts.ProfileType,
					Start:         start,
					End:           end,
					MaxNodes:      opts.MaxNodes,
					TraceIDs:      opts.TraceIDs,
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

			req := pyroscope.QueryRequest{
				LabelSelector:      expr,
				ProfileTypeID:      opts.ProfileType,
				Start:              start,
				End:                end,
				MaxNodes:           opts.resolveMaxNodes(cmd.Flags()),
				ProfileIDs:         opts.ProfileIDs,
				SpanIDs:            opts.SpanIDs,
				TraceIDs:           opts.TraceIDs,
				StackTraceSelector: opts.stackTraceSelector(),
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if opts.shared.IO.OutputFormat == "table" {
				return pyroscope.FormatQueryTable(cmd.OutOrStdout(), resp)
			}

			return opts.shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources pyroscope query -d UID '{service_name="frontend"}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h -o json`,
	}

	opts.setup(cmd.Flags())

	return cmd
}

// isUUID checks whether s is a valid UUID (8-4-4-4-12 hex format).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func isHexID(s string, length int) bool {
	if len(s) != length {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
