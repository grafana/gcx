package reports

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/graph"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// ReportStatusResult holds aggregated health data for a single report.
type ReportStatusResult struct {
	Name           string                     `json:"name"`
	UUID           string                     `json:"uuid"`
	TimeSpan       string                     `json:"timeSpan"`
	SLOCount       int                        `json:"sloCount"`
	CombinedSLI    *float64                   `json:"combinedSli,omitempty"`
	CombinedBudget *float64                   `json:"combinedBudget,omitempty"`
	Status         string                     `json:"status"`
	SLOs           []definitions.StatusResult `json:"slos,omitempty"`
}

// ---------------------------------------------------------------------------
// status command
// ---------------------------------------------------------------------------

type reportStatusOpts struct {
	IO cmdio.Options
}

func (o *reportStatusOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ReportStatusTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ReportStatusTableCodec{Wide: true})
	o.IO.RegisterCustomCodec("graph", &ReportStatusGraphCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newStatusCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &reportStatusOpts{}
	cmd := &cobra.Command{
		Use:   "status [UUID]",
		Short: "Show SLO report status with combined SLI and error budget data.",
		Long: `Show SLO report status by aggregating health data across all SLOs in each report.

Fetches report definitions, resolves referenced SLO UUIDs, queries Prometheus
metrics, and computes combined SLI and error budget per report.`,
		Example: `  # Show status of all SLO reports.
  gcx slo reports status

  # Show status of a specific report by UUID.
  gcx slo reports status abc123def

  # Show extended status with per-SLO breakdown.
  gcx slo reports status -o wide

  # Output status as JSON for scripting.
  gcx slo reports status -o json

  # Render a combined SLI bar chart.
  gcx slo reports status -o graph`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
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

			// Fetch reports and all SLO definitions in parallel.
			var (
				reports []Report
				allSLOs []definitions.Slo
			)

			initG, initCtx := errgroup.WithContext(ctx)
			initG.Go(func() error {
				if len(args) == 1 {
					r, err := reportClient.Get(initCtx, args[0])
					if err != nil {
						return err
					}
					reports = []Report{*r}
					return nil
				}
				var err error
				reports, err = reportClient.List(initCtx)
				return err
			})
			initG.Go(func() error {
				var err error
				allSLOs, err = sloClient.List(initCtx)
				return err
			})
			if err := initG.Wait(); err != nil {
				return err
			}

			if len(reports) == 0 {
				cmdio.Info(cmd.OutOrStdout(), "No SLO reports found.")
				return nil
			}

			// Collect all unique SLO UUIDs across reports.
			uuidSet := make(map[string]struct{})
			for _, r := range reports {
				for _, s := range r.ReportDefinition.Slos {
					uuidSet[s.SloUUID] = struct{}{}
				}
			}

			// Index SLO definitions by UUID.
			sloIndex := make(map[string]definitions.Slo, len(allSLOs))
			for _, s := range allSLOs {
				sloIndex[s.UUID] = s
			}

			// Fetch Prometheus metrics for referenced SLOs.
			var referencedSLOs []definitions.Slo
			for uuid := range uuidSet {
				if s, ok := sloIndex[uuid]; ok {
					referencedSLOs = append(referencedSLOs, s)
				}
			}

			var metrics map[string]definitions.MetricData
			if len(referencedSLOs) > 0 {
				promClient, err := prometheus.NewClient(restCfg)
				if err != nil {
					return err
				}
				metrics = definitions.FetchMetrics(ctx, promClient, referencedSLOs)
			}

			// Build per-SLO status results indexed by UUID.
			sloResults := definitions.BuildStatusResults(referencedSLOs, metrics)
			sloResultIndex := make(map[string]definitions.StatusResult, len(sloResults))
			for _, sr := range sloResults {
				sloResultIndex[sr.UUID] = sr
			}

			// Build report-level status results.
			results := BuildReportStatusResults(reports, sloIndex, sloResultIndex)

			return opts.IO.Encode(cmd.OutOrStdout(), results)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// BuildReportStatusResults aggregates per-SLO status into per-report results.
func BuildReportStatusResults(
	reports []Report,
	sloIndex map[string]definitions.Slo,
	sloResultIndex map[string]definitions.StatusResult,
) []ReportStatusResult {
	results := make([]ReportStatusResult, 0, len(reports))

	for _, rpt := range reports {
		r := ReportStatusResult{
			Name:     rpt.Name,
			UUID:     rpt.UUID,
			TimeSpan: mapTimeSpan(rpt.TimeSpan),
			SLOCount: len(rpt.ReportDefinition.Slos),
		}

		// Collect per-SLO results for this report.
		var sloResults []definitions.StatusResult
		var slos []definitions.Slo
		for _, rs := range rpt.ReportDefinition.Slos {
			if sr, ok := sloResultIndex[rs.SloUUID]; ok {
				sloResults = append(sloResults, sr)
			}
			if s, ok := sloIndex[rs.SloUUID]; ok {
				slos = append(slos, s)
			}
		}
		r.SLOs = sloResults

		// Compute combined SLI (simple average of available SLIs).
		r.CombinedSLI = computeCombinedSLI(sloResults)

		// Compute combined budget from combined SLI and average objective.
		avgObjective := computeAverageObjective(sloResults)
		if r.CombinedSLI != nil && avgObjective > 0 {
			budget := definitions.ComputeBudget(*r.CombinedSLI, avgObjective)
			r.CombinedBudget = &budget
		}

		r.Status = computeReportStatus(slos, r.CombinedSLI, avgObjective)
		results = append(results, r)
	}

	return results
}

// computeCombinedSLI calculates the average SLI across SLOs with data.
func computeCombinedSLI(sloResults []definitions.StatusResult) *float64 {
	var sum float64
	var count int
	for _, sr := range sloResults {
		if sr.SLI != nil {
			sum += *sr.SLI
			count++
		}
	}
	if count == 0 {
		return nil
	}
	avg := sum / float64(count)
	return &avg
}

// computeAverageObjective returns the average objective across SLOs.
func computeAverageObjective(sloResults []definitions.StatusResult) float64 {
	var sum float64
	var count int
	for _, sr := range sloResults {
		if sr.Objective > 0 {
			sum += sr.Objective
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// computeReportStatus determines the report-level status.
// Lifecycle states from any SLO take priority, then combined SLI vs objective.
func computeReportStatus(slos []definitions.Slo, combinedSLI *float64, avgObjective float64) string {
	// Check for lifecycle states across all SLOs.
	for _, s := range slos {
		status := definitions.ComputeStatus(s, nil, 0)
		switch status {
		case "Creating", "Updating", "Deleting", "Error":
			return status
		}
	}

	if combinedSLI == nil {
		return "NODATA"
	}

	if *combinedSLI >= avgObjective {
		return "OK"
	}

	return "BREACHING"
}

// ---------------------------------------------------------------------------
// status table codec
// ---------------------------------------------------------------------------

// ReportStatusTableCodec renders ReportStatusResult data as a table.
type ReportStatusTableCodec struct {
	Wide bool
}

func (c *ReportStatusTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ReportStatusTableCodec) Encode(w io.Writer, v any) error {
	results, ok := v.([]ReportStatusResult)
	if !ok {
		return errors.New("invalid data type for report status table codec: expected []ReportStatusResult")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTIME_SPAN\tSLOS\tCOMBINED_SLI\tCOMBINED_BUDGET\tSTATUS")

	for _, r := range results {
		sliStr := formatOptionalPercent(r.CombinedSLI)
		budgetStr := formatOptionalBudget(r.CombinedBudget)
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
			r.Name, r.TimeSpan, r.SLOCount, sliStr, budgetStr, r.Status)

		if c.Wide {
			for _, slo := range r.SLOs {
				sloSLI := formatOptionalPercent(slo.SLI)
				sloBudget := formatOptionalBudget(slo.Budget)
				fmt.Fprintf(tw, "  %s\t\t\t%s\t%s\t%s\n",
					slo.Name, sloSLI, sloBudget, slo.Status)
			}
		}
	}

	return tw.Flush()
}

func (c *ReportStatusTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("report status table codec does not support decoding")
}

// ---------------------------------------------------------------------------
// status graph codec
// ---------------------------------------------------------------------------

type ReportStatusGraphCodec struct{}

func (c *ReportStatusGraphCodec) Format() format.Format {
	return "graph"
}

func (c *ReportStatusGraphCodec) Encode(w io.Writer, v any) error {
	results, ok := v.([]ReportStatusResult)
	if !ok {
		return errors.New("invalid data type for report status graph codec: expected []ReportStatusResult")
	}

	items := make([]graph.PercentageBarItem, 0, len(results))
	for _, r := range results {
		if r.CombinedSLI == nil {
			continue
		}
		target := 0.0
		if r.CombinedBudget != nil {
			target = computeAverageObjectiveFromSLOs(r.SLOs) * 100
		}
		items = append(items, graph.PercentageBarItem{
			Name:   r.Name,
			Value:  *r.CombinedSLI * 100,
			Target: target,
		})
	}

	if len(items) == 0 {
		fmt.Fprintln(w, "No metric data available for graph rendering.")
		return nil
	}

	opts := graph.DefaultChartOptions()
	return graph.RenderPercentageBars(w, "SLO Report Compliance Summary", items, opts)
}

func (c *ReportStatusGraphCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("report status graph codec does not support decoding")
}

// computeAverageObjectiveFromSLOs computes average objective from SLO status results.
func computeAverageObjectiveFromSLOs(slos []definitions.StatusResult) float64 {
	return computeAverageObjective(slos)
}

// ---------------------------------------------------------------------------
// formatting helpers (reuse patterns from definitions)
// ---------------------------------------------------------------------------

func formatOptionalPercent(v *float64) string {
	if v == nil {
		return "--"
	}
	return fmt.Sprintf("%.2f%%", *v*100)
}

func formatOptionalBudget(v *float64) string {
	if v == nil {
		return "--"
	}
	return fmt.Sprintf("%.1f%%", *v*100)
}
