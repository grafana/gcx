package pyroscope

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/graph"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type pyroscopeMetricsOpts struct {
	shared      dsquery.SharedOpts
	Datasource  string
	ProfileType string
	GroupBy     []string
	Aggregation string
	Limit       int64
	Top         bool
}

func (opts *pyroscopeMetricsOpts) setup(flags *pflag.FlagSet) {
	opts.shared.IO.RegisterCustomCodec("table", &pyroscopeSeriesTableCodec{})
	opts.shared.IO.RegisterCustomCodec("wide", &pyroscopeSeriesWideCodec{})
	opts.shared.IO.RegisterCustomCodec("graph", &pyroscopeSeriesGraphCodec{})
	opts.shared.IO.DefaultFormat("table")
	opts.shared.IO.BindFlags(flags)

	flags.StringVar(&opts.shared.From, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	flags.StringVar(&opts.shared.To, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	flags.StringVar(&opts.shared.Step, "step", "", "Query step (e.g., '15s', '1m'); defaults to the Pyroscope datasource minStep (or 15s) when omitted")
	flags.StringVar(&opts.shared.Since, "since", "", "Duration before --to (or now if omitted); mutually exclusive with --from")

	opts.shared.SetupExprFlag(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
	flags.BoolVar(&opts.Top, "top", false, "Aggregate into a ranked leaderboard (equivalent to profilecli query top)")
	flags.StringVar(&opts.ProfileType, "profile-type", "", "Profile type ID (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds') (required)")
	flags.StringSliceVar(&opts.GroupBy, "group-by", nil, "Group series by label (repeatable, defaults to service_name)")
	flags.StringVar(&opts.Aggregation, "aggregation", "", "Aggregation type: 'sum' or 'average'")
	flags.Int64Var(&opts.Limit, "limit", 10, "Maximum number of series to return")
}

func (opts *pyroscopeMetricsOpts) Validate() error {
	if err := opts.shared.Validate(); err != nil {
		return err
	}
	if opts.ProfileType == "" {
		return errors.New("--profile-type is required")
	}
	opts.Aggregation = strings.ToUpper(opts.Aggregation)
	if opts.Aggregation != "" && opts.Aggregation != "SUM" && opts.Aggregation != "AVERAGE" {
		return fmt.Errorf("--aggregation must be 'sum' or 'average', got %q", opts.Aggregation)
	}
	return nil
}

// MetricsCmd returns the `metrics` subcommand for a Pyroscope datasource parent.
func MetricsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pyroscopeMetricsOpts{}

	cmd := &cobra.Command{
		Use:   "metrics [EXPR]",
		Short: "Query profile time-series data from a Pyroscope datasource",
		Long: `Query profile time-series data via SelectSeries from a Pyroscope datasource.

Shows how a profile metric (e.g., CPU, memory) changes over time. Useful for
identifying performance regressions and trends before diving into flamegraphs.

Use --top to aggregate the time range into a ranked leaderboard of the heaviest
consumers (equivalent to profilecli query top). Without --top, returns raw
time-series data points for trend analysis.

EXPR is the label selector (e.g., '{service_name="frontend"}').
Datasource is resolved from -d flag or datasources.pyroscope in your context.`,
		Example: `
  # Top services by CPU usage (ranked leaderboard)
  gcx datasources pyroscope metrics '{}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
    --since 1h --top

  # Top 20 services by memory, grouped by namespace
  gcx datasources pyroscope metrics '{}' \
    --profile-type memory:inuse_space:bytes:space:bytes \
    --since 1h --top --group-by namespace --limit 20

  # CPU usage over the last hour with 1-minute resolution
  gcx datasources pyroscope metrics -d pyro-001 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
    --since 1h --step 1m

  # Line chart output
  gcx datasources pyroscope metrics '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
    --since 1h --step 1m -o graph`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
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
			start, end, step, err := opts.shared.ParseTimes(now)
			if err != nil {
				return err
			}

			// Default group-by to service_name when not specified, matching
			// profilecli behavior. Pyroscope only returns labels for fields
			// in group_by; without it, series have empty labels ({}).
			groupBy := opts.GroupBy
			if len(groupBy) == 0 {
				groupBy = []string{"service_name"}
			}

			// --top mode queries the full range to get one bucket per series.
			if opts.Top && (start.IsZero() || end.IsZero()) {
				start, end = pyroscope.DefaultTimeRange(start, end)
			}

			stepSeconds, err := resolveMetricsStepSeconds(ctx, cfg, datasourceUID, opts.Top, start, end, step)
			if err != nil {
				return err
			}

			client, err := pyroscope.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := pyroscope.SelectSeriesRequest{
				ProfileTypeID: opts.ProfileType,
				LabelSelector: expr,
				Start:         start,
				End:           end,
				GroupBy:       groupBy,
				Step:          stepSeconds,
				Aggregation:   opts.Aggregation,
				Limit:         opts.Limit,
			}

			resp, err := client.SelectSeries(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("metrics query failed: %w", err)
			}

			if opts.Top {
				topResp := pyroscope.AggregateTopSeries(resp, opts.ProfileType, groupBy, int(opts.Limit))
				return opts.shared.IO.Encode(cmd.OutOrStdout(), topResp)
			}

			return opts.shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources pyroscope metrics '{}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top -o json",
	}

	opts.setup(cmd.Flags())
	return cmd
}

func resolveMetricsStepSeconds(ctx context.Context, cfg internalconfig.NamespacedRESTConfig, datasourceUID string, top bool, start, end time.Time, step time.Duration) (float64, error) {
	switch {
	case top:
		// One bucket per series across the (already-defaulted) full range.
		return end.Sub(start).Seconds(), nil
	case step > 0:
		// Explicit --step wins over the datasource minStep.
		return step.Seconds(), nil
	default:
		minStep, err := dsquery.GetPyroscopeMinStep(ctx, cfg, datasourceUID)
		if err != nil {
			return 0, err
		}
		return minStep.Seconds(), nil
	}
}

// pyroscopeSeriesTableCodec renders SelectSeriesResponse or TopSeriesResponse as a table.
type pyroscopeSeriesTableCodec struct{}

func (c *pyroscopeSeriesTableCodec) Format() format.Format { return "table" }

func (c *pyroscopeSeriesTableCodec) Encode(w io.Writer, data any) error {
	switch resp := data.(type) {
	case *pyroscope.SelectSeriesResponse:
		return pyroscope.FormatSeriesTable(w, resp)
	case *pyroscope.TopSeriesResponse:
		return pyroscope.FormatTopSeriesTable(w, resp)
	default:
		return errors.New("invalid data type for series table codec")
	}
}

func (c *pyroscopeSeriesTableCodec) Decode(io.Reader, any) error {
	return errors.New("series table codec does not support decoding")
}

// pyroscopeSeriesWideCodec renders SelectSeriesResponse with labels exploded into columns.
type pyroscopeSeriesWideCodec struct{}

func (c *pyroscopeSeriesWideCodec) Format() format.Format { return "wide" }

func (c *pyroscopeSeriesWideCodec) Encode(w io.Writer, data any) error {
	switch resp := data.(type) {
	case *pyroscope.SelectSeriesResponse:
		return pyroscope.FormatSeriesTableWide(w, resp)
	case *pyroscope.TopSeriesResponse:
		return pyroscope.FormatTopSeriesTable(w, resp)
	default:
		return errors.New("invalid data type for series wide codec")
	}
}

func (c *pyroscopeSeriesWideCodec) Decode(io.Reader, any) error {
	return errors.New("series wide codec does not support decoding")
}

// pyroscopeSeriesGraphCodec renders SelectSeriesResponse as a terminal chart.
type pyroscopeSeriesGraphCodec struct{}

func (c *pyroscopeSeriesGraphCodec) Format() format.Format { return "graph" }

func (c *pyroscopeSeriesGraphCodec) Encode(w io.Writer, data any) error {
	switch resp := data.(type) {
	case *pyroscope.SelectSeriesResponse:
		chartData, err := graph.FromPyroscopeSeriesResponse(resp)
		if err != nil {
			return err
		}
		opts := graph.DefaultChartOptions()
		return graph.RenderChart(w, chartData, opts)
	case *pyroscope.TopSeriesResponse:
		chartData := graph.FromTopSeriesResponse(resp)
		opts := graph.DefaultChartOptions()
		return graph.RenderChart(w, chartData, opts)
	default:
		return errors.New("invalid data type for series graph codec")
	}
}

func (c *pyroscopeSeriesGraphCodec) Decode(io.Reader, any) error {
	return errors.New("series graph codec does not support decoding")
}
