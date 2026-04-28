package pyroscope

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Operation labels propagated through queryerror.APIError so the fail package's
// help-command suggester can distinguish these commands from `gcx profiles metrics`
// (which also calls SelectSeries under the hood).
const (
	opProfileExemplarsQuery = "profile exemplars query"
	opSpanExemplarsQuery    = "span exemplars query"
)

const defaultExemplarsProfileType = "process_cpu:cpu:nanoseconds:cpu:nanoseconds"

type pyroscopeExemplarsOpts struct {
	IO              cmdio.Options
	Time            dsquery.TimeRangeOpts
	Datasource      string
	Expr            string
	ProfileType     string
	TopN            int64
	MaxLabelColumns int
}

func (opts *pyroscopeExemplarsOpts) setup(flags *pflag.FlagSet, tableCodec format.Codec) {
	opts.IO.RegisterCustomCodec("table", tableCodec)
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	opts.Time.SetupTimeFlags(flags)
	flags.StringVar(&opts.Expr, "expr", "", "Label selector (alternative to positional argument)")
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
	flags.StringVar(&opts.ProfileType, "profile-type", defaultExemplarsProfileType, "Profile type ID")
	flags.Int64Var(&opts.TopN, "top-n", 100, "Maximum number of exemplars to return")
	flags.IntVar(&opts.MaxLabelColumns, "max-label-columns", 3, "Max label columns in table output (0 hides label columns)")
}

func (opts *pyroscopeExemplarsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if err := opts.Time.ValidateTimeRange(); err != nil {
		return err
	}
	if opts.ProfileType == "" {
		return errors.New("--profile-type is required")
	}
	if opts.TopN <= 0 {
		return errors.New("--top-n must be greater than 0")
	}
	if opts.MaxLabelColumns < 0 {
		return errors.New("--max-label-columns must be >= 0")
	}
	return nil
}

func (opts *pyroscopeExemplarsOpts) resolveExpr(args []string) (string, error) {
	haveFlag := opts.Expr != ""
	haveArg := len(args) > 0
	switch {
	case haveFlag && haveArg:
		return "", errors.New("provide the label selector as a positional argument or via --expr, not both")
	case !haveFlag && !haveArg:
		return "", errors.New("label selector is required: provide as positional argument or via --expr")
	case haveFlag:
		return opts.Expr, nil
	default:
		return args[0], nil
	}
}

// ExemplarsCmd returns the `exemplars` parent command with `profile` and
// `span` subcommands.
func ExemplarsCmd(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exemplars",
		Short: "Query profile or span exemplars from a Pyroscope datasource",
		Long: `Query profile or span exemplars from a Pyroscope datasource.

Exemplars link profile data to concrete samples or trace spans, enabling a pivot from
"which service was slow" to "which exact profile" (profile exemplars) or "which trace
span" (span exemplars).`,
	}
	cmd.AddCommand(exemplarsProfileCmd(loader))
	cmd.AddCommand(exemplarsSpanCmd(loader))
	return cmd
}

func exemplarsProfileCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pyroscopeExemplarsOpts{}
	cmd := &cobra.Command{
		Use:   "profile [EXPR]",
		Short: "List individual profile exemplars",
		Long: `List individual profile exemplars by calling SelectSeries with EXEMPLAR_TYPE_INDIVIDUAL.

Each row is a concrete profile sample identified by Profile ID. When profiles are
span-aware (e.g. via otelpyroscope), a Span ID column is included linking to the
associated trace span.

EXPR is the label selector (e.g. '{service_name="frontend"}').`,
		Example: `
  # Top profile exemplars in the last hour
  gcx datasources pyroscope exemplars profile '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # JSON output
  gcx datasources pyroscope exemplars profile '{}' --since 30m -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			expr, err := opts.resolveExpr(args)
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			var cfgCtx *internalconfig.Context
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err != nil {
				logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
			} else {
				cfgCtx = fullCfg.GetCurrentContext()
			}
			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "pyroscope")
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

			start, end, err := opts.Time.ParseTimeRange(time.Now())
			if err != nil {
				return err
			}
			start, end = pyroscope.DefaultTimeRange(start, end)

			client, err := pyroscope.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.SelectSeries(ctx, datasourceUID, pyroscope.SelectSeriesRequest{
				ProfileTypeID: opts.ProfileType,
				LabelSelector: expr,
				Start:         start,
				End:           end,
				Step:          autoStepSeconds(start, end, opts.TopN),
				ExemplarType:  pyroscope.ExemplarTypeIndividual,
			})
			if err != nil {
				return fmt.Errorf("profile exemplars query failed: %w", relabelAPIErrorOperation(err, opProfileExemplarsQuery))
			}

			result := pyroscope.BuildProfileExemplarsResult(resp, start, end, opts.ProfileType, int(opts.TopN))
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources pyroscope exemplars profile '{}' --profile-type " + defaultExemplarsProfileType + " --since 1h -o json",
	}

	opts.setup(cmd.Flags(), &profileExemplarsTableCodec{maxLabelColumns: &opts.MaxLabelColumns})
	return cmd
}

func exemplarsSpanCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pyroscopeExemplarsOpts{}
	cmd := &cobra.Command{
		Use:   "span [EXPR]",
		Short: "List span exemplars (profiles linked to trace spans)",
		Long: `List span exemplars by calling SelectHeatmap with HEATMAP_QUERY_TYPE_SPAN.

Each row is a span-linked profile sample identified by Span ID, which can be used to
pivot to the associated trace in Tempo. Requires span-aware instrumentation upstream
(e.g. otelpyroscope); without it the query returns an empty list.

EXPR is the label selector (e.g. '{service_name="frontend"}').`,
		Example: `
  # Top span exemplars in the last hour
  gcx datasources pyroscope exemplars span '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # JSON output, more label context
  gcx datasources pyroscope exemplars span '{}' --since 30m --max-label-columns 5 -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			expr, err := opts.resolveExpr(args)
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			var cfgCtx *internalconfig.Context
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err != nil {
				logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
			} else {
				cfgCtx = fullCfg.GetCurrentContext()
			}
			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "pyroscope")
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

			start, end, err := opts.Time.ParseTimeRange(time.Now())
			if err != nil {
				return err
			}
			start, end = pyroscope.DefaultTimeRange(start, end)

			client, err := pyroscope.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.SelectHeatmap(ctx, datasourceUID, pyroscope.SelectHeatmapRequest{
				ProfileTypeID: opts.ProfileType,
				LabelSelector: expr,
				Start:         start,
				End:           end,
				Step:          autoStepSeconds(start, end, opts.TopN),
				QueryType:     pyroscope.HeatmapQueryTypeSpan,
				ExemplarType:  pyroscope.ExemplarTypeSpan,
				Limit:         opts.TopN,
			})
			if err != nil {
				return fmt.Errorf("span exemplars query failed: %w", relabelAPIErrorOperation(err, opSpanExemplarsQuery))
			}

			result := pyroscope.BuildSpanExemplarsResult(resp, start, end, opts.ProfileType, int(opts.TopN))
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources pyroscope exemplars span '{}' --profile-type " + defaultExemplarsProfileType + " --since 1h -o json",
	}

	opts.setup(cmd.Flags(), &spanExemplarsTableCodec{maxLabelColumns: &opts.MaxLabelColumns})
	return cmd
}

// relabelAPIErrorOperation rewrites the Operation on an embedded
// queryerror.APIError so the fail package's help-command suggester can
// distinguish exemplars calls from other SelectSeries/SelectHeatmap callers.
// No-op for errors that aren't APIErrors.
func relabelAPIErrorOperation(err error, operation string) error {
	var apiErr *queryerror.APIError
	if errors.As(err, &apiErr) {
		apiErr.Operation = operation
	}
	return err
}

// autoStepSeconds divides the time range into topN buckets so that each
// bucket tends to carry at most one exemplar. Mirrors profilecli.
func autoStepSeconds(start, end time.Time, topN int64) float64 {
	if topN <= 0 {
		return 1
	}
	step := end.Sub(start).Seconds() / float64(topN)
	if step < 1 {
		step = 1
	}
	return step
}

// profileExemplarsTableCodec reads maxLabelColumns via pointer so flag parsing
// (which happens after codec registration) is reflected at Encode time.
type profileExemplarsTableCodec struct {
	maxLabelColumns *int
}

func (c *profileExemplarsTableCodec) Format() format.Format { return "table" }

func (c *profileExemplarsTableCodec) Encode(w io.Writer, data any) error {
	v, ok := data.(*pyroscope.ProfileExemplarsResult)
	if !ok {
		return errors.New("invalid data type for profile exemplars table codec")
	}
	return pyroscope.FormatProfileExemplarsTable(w, v, *c.maxLabelColumns)
}

func (c *profileExemplarsTableCodec) Decode(io.Reader, any) error {
	return errors.New("profile exemplars table codec does not support decoding")
}

type spanExemplarsTableCodec struct {
	maxLabelColumns *int
}

func (c *spanExemplarsTableCodec) Format() format.Format { return "table" }

func (c *spanExemplarsTableCodec) Encode(w io.Writer, data any) error {
	v, ok := data.(*pyroscope.SpanExemplarsResult)
	if !ok {
		return errors.New("invalid data type for span exemplars table codec")
	}
	return pyroscope.FormatSpanExemplarsTable(w, v, *c.maxLabelColumns)
}

func (c *spanExemplarsTableCodec) Decode(io.Reader, any) error {
	return errors.New("span exemplars table codec does not support decoding")
}
