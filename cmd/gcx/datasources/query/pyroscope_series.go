package query

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/graph"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type seriesOpts struct {
	shared      sharedQueryOpts
	ProfileType string
	GroupBy     []string
	Aggregation string
	Limit       int64
	Top         bool
}

func (opts *seriesOpts) setup(flags *pflag.FlagSet) {
	opts.shared.IO.RegisterCustomCodec("table", &seriesTableCodec{})
	opts.shared.IO.RegisterCustomCodec("wide", &seriesWideCodec{})
	opts.shared.IO.RegisterCustomCodec("graph", &seriesGraphCodec{})
	opts.shared.IO.DefaultFormat("table")
	opts.shared.IO.BindFlags(flags)

	flags.StringVar(&opts.shared.From, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	flags.StringVar(&opts.shared.To, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	flags.StringVar(&opts.shared.Step, "step", "", "Query step (e.g., '15s', '1m')")
	flags.StringVar(&opts.shared.Since, "since", "", "Duration before --to (or now if omitted); mutually exclusive with --from")

	flags.BoolVar(&opts.Top, "top", false, "Aggregate into a ranked leaderboard (equivalent to profilecli query top)")
	flags.StringVar(&opts.ProfileType, "profile-type", "", "Profile type ID (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds') (required)")
	flags.StringSliceVar(&opts.GroupBy, "group-by", nil, "Group series by label (repeatable, defaults to service_name)")
	flags.StringVar(&opts.Aggregation, "aggregation", "", "Aggregation type: 'sum' or 'average'")
	flags.Int64Var(&opts.Limit, "limit", 10, "Maximum number of series to return")
}

func (opts *seriesOpts) Validate() error {
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

// PyroscopeSeriesCmd returns the `series` subcommand for a Pyroscope datasource parent.
func PyroscopeSeriesCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &seriesOpts{}

	cmd := &cobra.Command{
		Use:   "series [DATASOURCE_UID] EXPR",
		Short: "Query profile time-series data from a Pyroscope datasource",
		Long: `Query profile time-series data via SelectSeries from a Pyroscope datasource.

Shows how a profile metric (e.g., CPU, memory) changes over time. Useful for
identifying performance regressions and trends before diving into flamegraphs.

Use --top to aggregate the time range into a ranked leaderboard of the heaviest
consumers (equivalent to profilecli query top). Without --top, returns raw
time-series data points for trend analysis.

DATASOURCE_UID is optional when datasources.pyroscope is configured in your context.
EXPR is the label selector (e.g., '{service_name="frontend"}').`,
		Example: `
  # Top services by CPU usage (ranked leaderboard)
  gcx datasources pyroscope series '{}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
    --since 1h --top

  # Top 20 services by memory, grouped by namespace
  gcx datasources pyroscope series '{}' \
    --profile-type memory:inuse_space:bytes:space:bytes \
    --since 1h --top --group-by namespace --limit 20

  # CPU usage over the last hour with 1-minute resolution
  gcx datasources pyroscope series '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
    --since 1h --step 1m

  # Group by namespace
  gcx datasources pyroscope series '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
    --since 1h --step 1m --group-by namespace

  # Line chart output
  gcx datasources pyroscope series '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
    --since 1h --step 1m -o graph`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
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
			start, end, step, err := opts.shared.parseTimes(now)
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

			// --top mode: set step to full range to get one bucket per series.
			var stepSeconds float64
			if opts.Top {
				if start.IsZero() || end.IsZero() {
					s, e := pyroscope.DefaultTimeRange(start, end)
					start, end = s, e
				}
				stepSeconds = end.Sub(start).Seconds()
			} else if step > 0 {
				stepSeconds = step.Seconds()
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
				return fmt.Errorf("series query failed: %w", err)
			}

			if opts.Top {
				topResp := pyroscope.AggregateTopSeries(resp, opts.ProfileType, groupBy, int(opts.Limit))
				return opts.shared.IO.Encode(cmd.OutOrStdout(), topResp)
			}

			return opts.shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

// seriesTableCodec renders SelectSeriesResponse or TopSeriesResponse as a table.
type seriesTableCodec struct{}

func (c *seriesTableCodec) Format() format.Format { return "table" }

func (c *seriesTableCodec) Encode(w io.Writer, data any) error {
	switch resp := data.(type) {
	case *pyroscope.SelectSeriesResponse:
		return pyroscope.FormatSeriesTable(w, resp)
	case *pyroscope.TopSeriesResponse:
		return pyroscope.FormatTopSeriesTable(w, resp)
	default:
		return errors.New("invalid data type for series table codec")
	}
}

func (c *seriesTableCodec) Decode(io.Reader, any) error {
	return errors.New("series table codec does not support decoding")
}

// seriesWideCodec renders SelectSeriesResponse with labels exploded into columns.
type seriesWideCodec struct{}

func (c *seriesWideCodec) Format() format.Format { return "wide" }

func (c *seriesWideCodec) Encode(w io.Writer, data any) error {
	switch resp := data.(type) {
	case *pyroscope.SelectSeriesResponse:
		return pyroscope.FormatSeriesTableWide(w, resp)
	case *pyroscope.TopSeriesResponse:
		return pyroscope.FormatTopSeriesTable(w, resp)
	default:
		return errors.New("invalid data type for series wide codec")
	}
}

func (c *seriesWideCodec) Decode(io.Reader, any) error {
	return errors.New("series wide codec does not support decoding")
}

// seriesGraphCodec renders SelectSeriesResponse as a terminal chart.
type seriesGraphCodec struct{}

func (c *seriesGraphCodec) Format() format.Format { return "graph" }

func (c *seriesGraphCodec) Encode(w io.Writer, data any) error {
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

func (c *seriesGraphCodec) Decode(io.Reader, any) error {
	return errors.New("series graph codec does not support decoding")
}
