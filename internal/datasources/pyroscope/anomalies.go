package pyroscope

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const defaultAnomaliesProfileType = "process_cpu:cpu:nanoseconds:cpu:nanoseconds"

type pyroscopeAnomaliesOpts struct {
	IO              cmdio.Options
	Time            dsquery.TimeRangeOpts
	Datasource      string
	Expr            string
	ProfileType     string
	AnomalyTypes    []string
	TopN            int64
	MaxLabelColumns int
}

func (opts *pyroscopeAnomaliesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &anomaliesTableCodec{maxLabelColumns: &opts.MaxLabelColumns})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	opts.Time.SetupTimeFlags(flags)
	flags.StringVar(&opts.Expr, "expr", "", "Label selector (alternative to positional argument)")
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
	flags.StringVar(&opts.ProfileType, "profile-type", defaultAnomaliesProfileType, "Profile type ID")
	flags.StringSliceVar(&opts.AnomalyTypes, "anomaly-type", []string{"stacktrace"}, "Anomaly source(s) to query. Only 'stacktrace' is supported today. Repeatable")
	flags.Int64Var(&opts.TopN, "top-n", 100, "Maximum number of anomalies to return")
	flags.IntVar(&opts.MaxLabelColumns, "max-label-columns", 3, "Max label columns in table output (0 hides label columns)")
}

func (opts *pyroscopeAnomaliesOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if err := opts.Time.ValidateTimeRange(); err != nil {
		return err
	}
	if opts.ProfileType == "" {
		return errors.New("--profile-type is required")
	}
	if len(opts.AnomalyTypes) == 0 {
		return errors.New("--anomaly-type must have at least one value")
	}
	if opts.TopN <= 0 {
		return errors.New("--top-n must be greater than 0")
	}
	if opts.MaxLabelColumns < 0 {
		return errors.New("--max-label-columns must be >= 0")
	}
	return nil
}

func (opts *pyroscopeAnomaliesOpts) resolveExpr(args []string) (string, error) {
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

// anomalyTypeEnum maps the CLI's lowercase --anomaly-type values to the
// querier.v1.AnomalyType enum names the wire protocol expects.
func anomalyTypeEnum(name string) (string, error) {
	switch name {
	case "stacktrace", pyroscope.AnomalyTypeStacktrace:
		return pyroscope.AnomalyTypeStacktrace, nil
	default:
		return "", fmt.Errorf("unknown --anomaly-type %q (supported: stacktrace)", name)
	}
}

func AnomaliesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pyroscopeAnomaliesOpts{}
	cmd := &cobra.Command{
		Use:   "anomalies [EXPR]",
		Short: "[experimental] Query profile anomalies from a Pyroscope datasource",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Query profiles flagged as anomalies by an external anomaly source and
confirmed present in ingested data for the given label selector and time range.

Requires the datasource's query-frontend to have an anomaly source configured
(query-frontend.anomaly-api.url); returns FAILED_PRECONDITION otherwise.

EXPR is the label selector (e.g. '{service_name="frontend"}'). It may resolve
to more than one service_name; anomalies from every matching service are
queried and confirmed in one call.`,
		Example: `
  # Anomalies for a service in the last hour
  gcx datasources pyroscope anomalies -d UID '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Every service in a namespace
  gcx datasources pyroscope anomalies -d UID '{namespace="prod"}' --since 1h

  # JSON output
  gcx datasources pyroscope anomalies -d UID '{service_name="frontend"}' --since 1h -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			expr, err := opts.resolveExpr(args)
			if err != nil {
				return err
			}

			anomalyTypes := make([]string, len(opts.AnomalyTypes))
			for i, t := range opts.AnomalyTypes {
				enum, err := anomalyTypeEnum(t)
				if err != nil {
					return err
				}
				anomalyTypes[i] = enum
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}
			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "pyroscope")
			if err != nil {
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

			resp, err := client.QueryAnomalies(ctx, datasourceUID, pyroscope.QueryAnomaliesRequest{
				ProfileTypeID: opts.ProfileType,
				LabelSelector: expr,
				Start:         start,
				End:           end,
				AnomalyTypes:  anomalyTypes,
			})
			if err != nil {
				return fmt.Errorf("anomalies query failed: %w", err)
			}

			result := pyroscope.BuildAnomaliesResult(resp, start, end, int(opts.TopN))
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources pyroscope anomalies -d UID '{service_name=\"frontend\"}' --since 1h -o json",
		agent.AnnotationStability: agent.StabilityExperimental,
	}

	opts.setup(cmd.Flags())
	return cmd
}

// anomaliesTableCodec reads maxLabelColumns via pointer so flag parsing
// (which happens after codec registration) is reflected at Encode time.
type anomaliesTableCodec struct {
	maxLabelColumns *int
}

func (c *anomaliesTableCodec) Format() format.Format { return "table" }

func (c *anomaliesTableCodec) Encode(w io.Writer, data any) error {
	v, ok := data.(*pyroscope.AnomaliesResult)
	if !ok {
		return errors.New("invalid data type for anomalies table codec")
	}
	return pyroscope.FormatAnomaliesTable(w, v, *c.maxLabelColumns)
}

func (c *anomaliesTableCodec) Decode(io.Reader, any) error {
	return errors.New("anomalies table codec does not support decoding")
}
