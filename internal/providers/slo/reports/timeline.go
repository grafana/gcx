package reports

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/graph"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ReportTimelinePayload is passed to report timeline codecs for encoding.
type ReportTimelinePayload struct {
	Reports  []Report
	SLOIndex map[string]definitions.Slo
	Points   map[string][]definitions.SLOTimeSeriesPoint // keyed by SLO UUID
	Start    time.Time
	End      time.Time
}

// ---------------------------------------------------------------------------
// timeline command
// ---------------------------------------------------------------------------

type reportTimelineOpts struct {
	IO    cmdio.Options
	From  string
	To    string
	Since string
	Step  string
}

func (o *reportTimelineOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ReportTimelineTableCodec{})
	o.IO.RegisterCustomCodec("graph", &ReportTimelineGraphCodec{})
	o.IO.DefaultFormat("graph")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.From, "from", "now-7d", "Start of the time range (e.g. now-7d, now-24h, RFC3339, Unix timestamp)")
	flags.StringVar(&o.To, "to", "now", "End of the time range (e.g. now, RFC3339, Unix timestamp)")
	flags.StringVar(&o.Since, "since", "", "Duration before now (e.g. 1h, 7d). Equivalent to --from now-<since> --to now.")
	flags.StringVar(&o.Step, "step", "", "Query step (e.g. 5m, 1h). Defaults to auto-computed value.")

	// Deprecated aliases for backward compatibility.
	flags.StringVar(&o.From, "start", "now-7d", "Deprecated: use --from instead")
	flags.StringVar(&o.To, "end", "now", "Deprecated: use --to instead")
	_ = flags.MarkDeprecated("start", "use --from instead")
	_ = flags.MarkDeprecated("end", "use --to instead")
}

func newTimelineCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &reportTimelineOpts{}
	cmd := &cobra.Command{
		Use:   "timeline [UUID]",
		Short: "Render SLI values over time for SLO reports.",
		Long: `Render SLI values over time as line charts for each SLO report by
executing range queries against the Prometheus datasource associated with
each constituent SLO.

Requires that SLO destination datasources have recording rules generating
grafana_slo_sli_window metrics.`,
		Example: `  # Render SLI trend for all SLO reports over the past 7 days.
  gcx slo reports timeline

  # Render SLI trend for a specific report.
  gcx slo reports timeline abc123def

  # Custom time range with explicit step.
  gcx slo reports timeline --from now-24h --to now --step 5m

  # Use duration shorthand for the past 24 hours.
  gcx slo reports timeline --since 24h

  # Output timeline data as a table.
  gcx slo reports timeline -o table`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			// Validate flag combinations.
			if err := definitions.ValidateTimelineFlags(cmd); err != nil {
				return err
			}

			// Apply --since shorthand.
			if cmd.Flags().Changed("since") {
				opts.From = "now-" + opts.Since
				opts.To = "now"
			}

			ctx := cmd.Context()

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			// Create report and SLO definition clients.
			reportClient, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			sloClient, err := definitions.NewClient(restCfg)
			if err != nil {
				return err
			}

			// Fetch report(s).
			var rpts []Report
			if len(args) == 1 {
				r, err := reportClient.Get(ctx, args[0])
				if err != nil {
					return err
				}
				rpts = []Report{*r}
			} else {
				rpts, err = reportClient.List(ctx)
				if err != nil {
					return err
				}
			}

			if len(rpts) == 0 {
				cmdio.Info(cmd.OutOrStdout(), "No SLO reports found.")
				return nil
			}

			// Collect all unique SLO UUIDs across all reports.
			uuidSet := make(map[string]struct{})
			for _, r := range rpts {
				for _, s := range r.ReportDefinition.Slos {
					uuidSet[s.SloUUID] = struct{}{}
				}
			}

			// Fetch all SLO definitions and index by UUID.
			allSLOs, err := sloClient.List(ctx)
			if err != nil {
				return err
			}
			sloIndex := make(map[string]definitions.Slo, len(allSLOs))
			for _, s := range allSLOs {
				sloIndex[s.UUID] = s
			}

			// Collect only the SLOs referenced by the reports.
			var referencedSLOs []definitions.Slo
			for uuid := range uuidSet {
				if s, ok := sloIndex[uuid]; ok {
					referencedSLOs = append(referencedSLOs, s)
				}
			}

			// Parse time range.
			now := time.Now()
			start, err := parseReportTimelineTime(opts.From, now)
			if err != nil {
				return fmt.Errorf("invalid --from: %w", err)
			}
			end, err := parseReportTimelineTime(opts.To, now)
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
				step = definitions.AutoStep(start, end)
			}

			// Create Prometheus client and fetch range metrics.
			var points map[string][]definitions.SLOTimeSeriesPoint
			if len(referencedSLOs) > 0 {
				promClient, err := prometheus.NewClient(restCfg)
				if err != nil {
					return err
				}
				points = definitions.FetchMetricsRange(ctx, promClient, referencedSLOs, start, end, step)
			} else {
				points = make(map[string][]definitions.SLOTimeSeriesPoint)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), ReportTimelinePayload{
				Reports:  rpts,
				SLOIndex: sloIndex,
				Points:   points,
				Start:    start,
				End:      end,
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// parseReportTimelineTime delegates to the definitions package's time parsing.
func parseReportTimelineTime(s string, now time.Time) (time.Time, error) {
	return definitions.ParseTimelineTime(s, now)
}

// ---------------------------------------------------------------------------
// report timeline graph codec
// ---------------------------------------------------------------------------

// ReportTimelineGraphCodec renders ReportTimelinePayload as line charts.
// It emits one chart per report, each containing the constituent SLOs.
type ReportTimelineGraphCodec struct{}

// Format returns the codec format identifier.
func (c *ReportTimelineGraphCodec) Format() format.Format { return "graph" }

// Encode writes per-report SLI trend line charts.
func (c *ReportTimelineGraphCodec) Encode(w io.Writer, v any) error {
	payload, ok := v.(ReportTimelinePayload)
	if !ok {
		return fmt.Errorf("reportTimelineGraphCodec: expected ReportTimelinePayload, got %T", v)
	}

	anyData := false
	for _, rpt := range payload.Reports {
		// Collect SLOs and points for this report.
		var reportSLOs []definitions.Slo
		reportPoints := make(map[string][]definitions.SLOTimeSeriesPoint)

		for _, rs := range rpt.ReportDefinition.Slos {
			slo, ok := payload.SLOIndex[rs.SloUUID]
			if !ok {
				continue
			}
			reportSLOs = append(reportSLOs, slo)
			if pts, ok := payload.Points[rs.SloUUID]; ok {
				reportPoints[rs.SloUUID] = pts
			}
		}

		chartData := definitions.FromSLOSLITrend(reportPoints)
		if len(chartData.Series) == 0 {
			continue
		}
		anyData = true

		// Build lookup for last-point compliance coloring.
		lastObjective := make(map[string]float64)
		lastValue := make(map[string]float64)
		for uuid, pts := range reportPoints {
			if len(pts) > 0 {
				last := pts[len(pts)-1]
				lastObjective[uuid] = last.Objective
				lastValue[uuid] = last.Value
			}
		}

		// Map SLO name → UUID.
		nameToUUID := make(map[string]string, len(reportSLOs))
		for _, slo := range reportSLOs {
			nameToUUID[slo.Name] = slo.UUID
		}

		// Assign compliance colors.
		for i, s := range chartData.Series {
			uuid, ok := nameToUUID[s.Name]
			if !ok {
				continue
			}
			obj, hasObj := lastObjective[uuid]
			val, hasVal := lastValue[uuid]
			if hasObj && hasVal && obj > 0 {
				chartData.Series[i].Color = graph.ComplianceColor(val*100, obj*100)
			}
		}

		// Override chart title to include the report name.
		chartData.Title = "SLO SLI Trend \u2014 " + rpt.Name

		opts := graph.DefaultChartOptions()
		if err := graph.RenderLineChart(w, chartData, opts); err != nil {
			return fmt.Errorf("failed to render chart for report %s: %w", rpt.Name, err)
		}
		fmt.Fprintln(w)
	}

	if !anyData {
		fmt.Fprintln(w, "No time-series data available.")
	}

	return nil
}

// Decode is not supported for the report timeline graph codec.
func (c *ReportTimelineGraphCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("reportTimelineGraphCodec: decode not supported")
}

// ---------------------------------------------------------------------------
// report timeline table codec
// ---------------------------------------------------------------------------

// ReportTimelineTableCodec renders ReportTimelinePayload as a tabular table.
type ReportTimelineTableCodec struct{}

// Format returns the codec format identifier.
func (c *ReportTimelineTableCodec) Format() format.Format { return "table" }

// Encode writes per-report SLI trends as a table with one row per (report, SLO, timestamp).
func (c *ReportTimelineTableCodec) Encode(w io.Writer, v any) error {
	payload, ok := v.(ReportTimelinePayload)
	if !ok {
		return fmt.Errorf("reportTimelineTableCodec: expected ReportTimelinePayload, got %T", v)
	}

	t := style.NewTable("REPORT", "NAME", "UUID", "TIMESTAMP", "SLI", "OBJECTIVE")

	for _, rpt := range payload.Reports {
		for _, rs := range rpt.ReportDefinition.Slos {
			pts, ok := payload.Points[rs.SloUUID]
			if !ok {
				continue
			}
			for _, pt := range pts {
				sliStr := fmt.Sprintf("%.4f%%", pt.Value*100)
				objStr := fmt.Sprintf("%.2f%%", pt.Objective*100)
				t.Row(
					rpt.Name,
					pt.Name,
					pt.UUID,
					pt.Time.Format(time.RFC3339),
					sliStr,
					objStr,
				)
			}
		}
	}

	return t.Render(w)
}

// Decode is not supported for the report timeline table codec.
func (c *ReportTimelineTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("reportTimelineTableCodec: decode not supported")
}
