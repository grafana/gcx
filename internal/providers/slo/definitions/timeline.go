package definitions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/grafana/grafanactl/internal/format"
	"github.com/grafana/grafanactl/internal/graph"
	cmdio "github.com/grafana/grafanactl/internal/output"
	"github.com/grafana/grafanactl/internal/query/prometheus"
	"github.com/grafana/promql-builder/go/promql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SLITrendPayload is passed to timeline codecs for encoding.
type SLITrendPayload struct {
	SLOs   []Slo
	Points map[string][]SLOTimeSeriesPoint // keyed by SLO UUID
	Start  time.Time
	End    time.Time
}

// ---------------------------------------------------------------------------
// timeline command
// ---------------------------------------------------------------------------

type timelineOpts struct {
	IO     cmdio.Options
	From   string
	To     string
	Window string
	Step   string
}

func (o *timelineOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TimelineTableCodec{})
	o.IO.RegisterCustomCodec("graph", &TimelineGraphCodec{})
	o.IO.DefaultFormat("graph")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.From, "from", "now-7d", "Start of the time range (e.g. now-7d, now-24h, RFC3339, Unix timestamp)")
	flags.StringVar(&o.To, "to", "now", "End of the time range (e.g. now, RFC3339, Unix timestamp)")
	flags.StringVar(&o.Window, "window", "", "Time window shorthand (e.g. 1h, 7d). Equivalent to --from now-<window> --to now.")
	flags.StringVar(&o.Step, "step", "", "Query step (e.g. 5m, 1h). Defaults to auto-computed value.")

	// Deprecated aliases for backward compatibility.
	flags.StringVar(&o.From, "start", "now-7d", "Deprecated: use --from instead")
	flags.StringVar(&o.To, "end", "now", "Deprecated: use --to instead")
	_ = flags.MarkDeprecated("start", "use --from instead")
	_ = flags.MarkDeprecated("end", "use --to instead")
}

// ValidateTimelineFlags checks that --window and --from/--to are not used together.
// This is exported so that the reports package can reuse the same validation logic.
func ValidateTimelineFlags(cmd *cobra.Command) error {
	windowSet := cmd.Flags().Changed("window")
	fromToSet := cmd.Flags().Changed("from") || cmd.Flags().Changed("to") ||
		cmd.Flags().Changed("start") || cmd.Flags().Changed("end")
	if windowSet && fromToSet {
		return errors.New("--window and --from/--to are mutually exclusive")
	}
	return nil
}

func newTimelineCommand() *cobra.Command {
	opts := &timelineOpts{}
	cmd := &cobra.Command{
		Use:   "timeline [UUID]",
		Short: "Render SLI values over time as a line chart.",
		Long: `Render SLI values over time as a line chart by executing a range query
against the Prometheus datasource associated with each SLO.

Requires that the SLO destination datasource has recording rules generating
grafana_slo_sli_window metrics.`,
		Example: `  # Render SLI trend for all SLOs over the past 7 days.
  grafanactl slo definitions timeline

  # Render SLI trend for a specific SLO.
  grafanactl slo definitions timeline abc123def

  # Custom time range with explicit step.
  grafanactl slo definitions timeline --from now-24h --to now --step 5m

  # Use window shorthand for the past 24 hours.
  grafanactl slo definitions timeline --window 24h

  # Output timeline data as a table.
  grafanactl slo definitions timeline -o table`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			// Validate flag combinations.
			if err := ValidateTimelineFlags(cmd); err != nil {
				return err
			}

			// Apply --window shorthand.
			if cmd.Flags().Changed("window") {
				opts.From = "now-" + opts.Window
				opts.To = "now"
			}

			ctx := cmd.Context()

			crud, cfg, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			// Fetch SLO definition(s).
			var slos []Slo
			if len(args) == 1 {
				s, err := crud.Get(ctx, args[0])
				if err != nil {
					return err
				}
				slos = []Slo{s.Spec}
			} else {
				typedObjs, err := crud.List(ctx)
				if err != nil {
					return err
				}
				slos = make([]Slo, len(typedObjs))
				for i := range typedObjs {
					slos[i] = typedObjs[i].Spec
				}
			}

			if len(slos) == 0 {
				cmdio.Info(cmd.OutOrStdout(), "No SLO definitions found.")
				return nil
			}

			// Parse time range.
			now := time.Now()
			start, err := parseTimelineTime(opts.From, now)
			if err != nil {
				return fmt.Errorf("invalid --from: %w", err)
			}
			end, err := parseTimelineTime(opts.To, now)
			if err != nil {
				return fmt.Errorf("invalid --to: %w", err)
			}

			// Compute step.
			var step time.Duration
			if opts.Step != "" {
				step, err = time.ParseDuration(opts.Step)
				if err != nil {
					return fmt.Errorf("invalid --step: %w", err)
				}
			} else {
				step = AutoStep(start, end)
			}

			// Create Prometheus client for range queries.
			promClient, err := prometheus.NewClient(cfg)
			if err != nil {
				return err
			}

			points := FetchMetricsRange(ctx, promClient, slos, start, end, step)

			return opts.IO.Encode(cmd.OutOrStdout(), SLITrendPayload{
				SLOs:   slos,
				Points: points,
				Start:  start,
				End:    end,
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// AutoStep calculates a reasonable query step for the given time range,
// targeting ~200 data points. The minimum step is 1 minute.
func AutoStep(start, end time.Time) time.Duration {
	const targetPoints = 200
	const minStep = time.Minute

	d := end.Sub(start)
	step := max(d/targetPoints, minStep)

	return step.Truncate(time.Minute)
}

// FetchMetricsRange batch-fetches Prometheus range metrics for the given SLOs.
// SLOs are grouped by destination datasource UID to minimise queries.
// Returns a map from SLO UUID to time series points. Errors are handled
// gracefully — failed queries result in missing entries.
func FetchMetricsRange(
	ctx context.Context,
	client *prometheus.Client,
	slos []Slo,
	start, end time.Time,
	step time.Duration,
) map[string][]SLOTimeSeriesPoint {
	result := make(map[string][]SLOTimeSeriesPoint)

	// Build a quick-lookup index: UUID → Slo.
	sloIndex := make(map[string]Slo, len(slos))
	for _, s := range slos {
		sloIndex[s.UUID] = s
	}

	// Group SLOs by destination datasource UID.
	groups := make(map[string][]Slo)
	for _, s := range slos {
		dsUID := ""
		if s.DestinationDatasource != nil {
			dsUID = s.DestinationDatasource.UID
		}
		groups[dsUID] = append(groups[dsUID], s)
	}

	for dsUID, groupSlos := range groups {
		if dsUID == "" {
			continue // Skip SLOs with no destination datasource.
		}

		uuids := make([]string, len(groupSlos))
		for i, s := range groupSlos {
			uuids[i] = s.UUID
		}
		uuidRegex := strings.Join(uuids, "|")

		// Fetch SLI window values as a range query.
		// Wrap with avg by (grafana_slo_uuid) to collapse any extra labels on the
		// recording rules (e.g. version, cluster) into a single series per SLO.
		expr, err := promql.Avg(
			promql.Vector("grafana_slo_sli_window").LabelMatchRegexp(uuidLabel, uuidRegex),
		).By([]string{uuidLabel}).Build()
		if err != nil {
			continue
		}
		q := expr.String()

		resp := queryRangeMetric(ctx, client, dsUID, q, start, end, step)
		if resp == nil {
			continue
		}

		for _, sample := range resp.Data.Result {
			uuid := sample.Metric[uuidLabel]
			slo, ok := sloIndex[uuid]
			if !ok {
				continue
			}

			objective := 0.0
			if len(slo.Objectives) > 0 {
				objective = slo.Objectives[0].Value
			}

			meta := SLOMetricPoint{
				UUID:      uuid,
				Name:      slo.Name,
				Objective: objective,
			}

			pts := ParseMatrixValues(sample.Values, meta, time.Now())
			if len(pts) > 0 {
				result[uuid] = append(result[uuid], pts...)
			}
		}
	}

	return result
}

// queryRangeMetric executes a range PromQL query and returns the response.
// Returns nil on error (graceful degradation).
func queryRangeMetric(
	ctx context.Context,
	client *prometheus.Client,
	dsUID, query string,
	start, end time.Time,
	step time.Duration,
) *prometheus.QueryResponse {
	resp, err := client.Query(ctx, dsUID, prometheus.QueryRequest{
		Query: query,
		Start: start,
		End:   end,
		Step:  step,
	})
	if err != nil {
		return nil
	}
	if resp.Status != "success" {
		return nil
	}
	return resp
}

// ParseMatrixValues converts the [][]any Values field from a matrix query sample
// into SLOTimeSeriesPoint slice. Each element of values is [timestamp float64, value string].
// Invalid or NaN values are skipped. The meta argument provides the UUID, Name, and
// Objective that every resulting point will carry.
//
// The now parameter is unused but retained to make the time dependency explicit
// for callers that may want to record the processing time.
func ParseMatrixValues(values [][]any, meta SLOMetricPoint, _ time.Time) []SLOTimeSeriesPoint {
	pts := make([]SLOTimeSeriesPoint, 0, len(values))

	for _, v := range values {
		if len(v) < 2 {
			continue
		}

		// Timestamp is always float64 (Unix seconds).
		ts, ok := v[0].(float64)
		if !ok {
			continue
		}

		// Value is a string representation of the float.
		val, err := parseMatrixValue(v[1])
		if err != nil {
			continue
		}

		pts = append(pts, SLOTimeSeriesPoint{
			SLOMetricPoint: SLOMetricPoint{
				UUID:      meta.UUID,
				Name:      meta.Name,
				Value:     val,
				Objective: meta.Objective,
			},
			Time: time.Unix(int64(ts), 0),
		})
	}

	return pts
}

// parseMatrixValue extracts a float64 value from an any (string or float64).
// Returns an error if the value is NaN, Inf, or cannot be parsed.
func parseMatrixValue(raw any) (float64, error) {
	var val float64

	switch v := raw.(type) {
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, err
		}
		val = f
	case float64:
		val = v
	default:
		return 0, fmt.Errorf("unexpected value type %T", raw)
	}

	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0, errors.New("value is NaN or Inf")
	}

	return val, nil
}

// ParseTimelineTime parses a time string for timeline commands.
// Supports "now", "now-Xd", "now-Xh", RFC3339, and Unix timestamps.
// This is exported so that the reports package can reuse the same parsing logic.
func ParseTimelineTime(s string, now time.Time) (time.Time, error) {
	return parseTimelineTime(s, now)
}

// parseTimelineTime parses a time string for the timeline command.
// Supports "now", "now-Xd", "now-Xh", RFC3339, and Unix timestamps.
func parseTimelineTime(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return now, nil
	}

	s = strings.TrimSpace(s)

	// Relative time: now, now-1h, now-7d, now+1h, etc.
	if strings.HasPrefix(s, "now") {
		return parseRelativeTimelineTime(s, now)
	}

	// RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Unix timestamp (integer or float)
	if ts, err := strconv.ParseFloat(s, 64); err == nil {
		sec := int64(ts)
		nsec := int64((ts - float64(sec)) * 1e9)
		return time.Unix(sec, nsec), nil
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %q", s)
}

// parseRelativeTimelineTime handles "now", "now-1h", "now+7d", etc.
func parseRelativeTimelineTime(s string, now time.Time) (time.Time, error) {
	if s == "now" {
		return now, nil
	}

	// Strip "now" prefix and parse sign + value + unit.
	rest := s[3:] // characters after "now"
	if len(rest) == 0 {
		return now, nil
	}

	sign := 1
	switch rest[0] {
	case '-':
		sign = -1
		rest = rest[1:]
	case '+':
		rest = rest[1:]
	default:
		return time.Time{}, fmt.Errorf("invalid relative time format: %q", s)
	}

	if len(rest) == 0 {
		return time.Time{}, fmt.Errorf("invalid relative time format: %q", s)
	}

	// Extract numeric part.
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 {
		return time.Time{}, fmt.Errorf("invalid relative time format: %q", s)
	}

	value, err := strconv.Atoi(rest[:i])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid relative time format: %q", s)
	}
	unit := rest[i:]

	var dur time.Duration
	switch unit {
	case "s":
		dur = time.Duration(value) * time.Second
	case "m":
		dur = time.Duration(value) * time.Minute
	case "h":
		dur = time.Duration(value) * time.Hour
	case "d":
		dur = time.Duration(value) * 24 * time.Hour
	case "w":
		dur = time.Duration(value) * 7 * 24 * time.Hour
	case "M":
		dur = time.Duration(value) * 30 * 24 * time.Hour
	case "y":
		dur = time.Duration(value) * 365 * 24 * time.Hour
	default:
		return time.Time{}, fmt.Errorf("unknown time unit %q in %q", unit, s)
	}

	return now.Add(time.Duration(sign) * dur), nil
}

// ---------------------------------------------------------------------------
// timeline graph codec
// ---------------------------------------------------------------------------

// TimelineGraphCodec renders SLITrendPayload as a line chart.
type TimelineGraphCodec struct{}

// Format returns the codec format identifier.
func (c *TimelineGraphCodec) Format() format.Format { return "graph" }

// Encode writes the SLI trend as one line chart per SLO.
func (c *TimelineGraphCodec) Encode(w io.Writer, v any) error {
	payload, ok := v.(SLITrendPayload)
	if !ok {
		return fmt.Errorf("timelineGraphCodec: expected SLITrendPayload, got %T", v)
	}

	if len(payload.Points) == 0 {
		fmt.Fprintln(w, "No time-series data available.")
		return nil
	}

	opts := graph.DefaultChartOptions()

	for _, slo := range payload.SLOs {
		pts, ok := payload.Points[slo.UUID]
		if !ok || len(pts) == 0 {
			continue
		}

		chartData := FromSLOSLITrend(map[string][]SLOTimeSeriesPoint{slo.UUID: pts})
		if len(chartData.Series) == 0 {
			continue
		}

		// Color the line by current compliance status (last point).
		last := pts[len(pts)-1]
		if last.Objective > 0 {
			// last.Value is a ratio (0–1); ComplianceColor expects percentages (0–100).
			chartData.Series[0].Color = graph.ComplianceColor(last.Value*100, last.Objective*100)
		}

		chartData.Title = slo.Name
		if err := graph.RenderLineChart(w, chartData, opts); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}

	return nil
}

// Decode is not supported for the timeline graph codec.
func (c *TimelineGraphCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("timelineGraphCodec: decode not supported")
}

// ---------------------------------------------------------------------------
// timeline table codec
// ---------------------------------------------------------------------------

// TimelineTableCodec renders SLITrendPayload as a tabular table.
type TimelineTableCodec struct{}

// Format returns the codec format identifier.
func (c *TimelineTableCodec) Format() format.Format { return "table" }

// Encode writes the SLI trend as a table with one row per (SLO, timestamp).
func (c *TimelineTableCodec) Encode(w io.Writer, v any) error {
	payload, ok := v.(SLITrendPayload)
	if !ok {
		return fmt.Errorf("timelineTableCodec: expected SLITrendPayload, got %T", v)
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tUUID\tTIMESTAMP\tSLI\tOBJECTIVE")

	// Emit rows grouped by UUID, preserving SLOs slice order.
	for _, slo := range payload.SLOs {
		pts, ok := payload.Points[slo.UUID]
		if !ok {
			continue
		}
		for _, pt := range pts {
			sliStr := fmt.Sprintf("%.4f%%", pt.Value*100)
			objStr := fmt.Sprintf("%.2f%%", pt.Objective*100)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				pt.Name,
				pt.UUID,
				pt.Time.Format(time.RFC3339),
				sliStr,
				objStr,
			)
		}
	}

	return tw.Flush()
}

// Decode is not supported for the timeline table codec.
func (c *TimelineTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("timelineTableCodec: decode not supported")
}
