package definitions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/grafana/grafanactl/internal/format"
	"github.com/grafana/grafanactl/internal/graph"
	cmdio "github.com/grafana/grafanactl/internal/output"
	"github.com/grafana/grafanactl/internal/query/prometheus"
	"github.com/grafana/promql-builder/go/promql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

const uuidLabel = "grafana_slo_uuid"

// StatusResult holds merged SLO API + metric data for a single SLO.
type StatusResult struct {
	Name      string   `json:"name"`
	UUID      string   `json:"uuid"`
	Objective float64  `json:"objective"`
	Window    string   `json:"window"`
	SLI       *float64 `json:"sli,omitempty"`
	Budget    *float64 `json:"budget,omitempty"`
	BurnRate  *float64 `json:"burnRate,omitempty"`
	SLI1h     *float64 `json:"sli1h,omitempty"`
	SLI1d     *float64 `json:"sli1d,omitempty"`
	Status    string   `json:"status"`
}

// MetricData holds the parsed PromQL results for a single SLO UUID.
type MetricData struct {
	SLI      *float64
	BurnRate *float64
	SLI1h    *float64
	SLI1d    *float64
}

// ---------------------------------------------------------------------------
// status command
// ---------------------------------------------------------------------------

type statusOpts struct {
	IO cmdio.Options
}

func (o *statusOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &StatusTableCodec{})
	o.IO.RegisterCustomCodec("wide", &StatusTableCodec{Wide: true})
	o.IO.RegisterCustomCodec("graph", &statusGraphCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newStatusCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &statusOpts{}
	cmd := &cobra.Command{
		Use:   "status [UUID]",
		Short: "Show SLO definitions status with SLI and error budget data.",
		Long: `Show SLO definitions status by combining the SLO API with Prometheus metrics.

Displays current SLI, error budget, and health status for each SLO definition.
Requires that the SLO destination datasource has recording rules generating
grafana_slo_* metrics.`,
		Example: `  # Show status of all SLO definitions.
  grafanactl slo definitions status

  # Show status of a specific SLO by UUID.
  grafanactl slo definitions status abc123def

  # Show extended status with 1h/1d SLI columns.
  grafanactl slo definitions status -o wide

  # Output status as JSON for scripting.
  grafanactl slo definitions status -o json

  # Render a compliance summary bar chart.
  grafanactl slo definitions status -o graph`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, cfg, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			// Fetch SLO definition(s).
			var slos []Slo
			if len(args) == 1 {
				typedObj, err := crud.Get(ctx, args[0])
				if err != nil {
					return err
				}
				slos = []Slo{typedObj.Spec}
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

			// Create Prometheus client for metric queries.
			promClient, err := prometheus.NewClient(cfg)
			if err != nil {
				return err
			}

			metrics := FetchMetrics(ctx, promClient, slos)

			// Merge SLO data with metrics.
			results := BuildStatusResults(slos, metrics)

			return opts.IO.Encode(cmd.OutOrStdout(), results)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// BuildStatusResults merges SLO definitions with their metric data.
func BuildStatusResults(slos []Slo, metrics map[string]MetricData) []StatusResult {
	results := make([]StatusResult, 0, len(slos))

	for _, s := range slos {
		objective := 0.0
		window := "-"
		if len(s.Objectives) > 0 {
			objective = s.Objectives[0].Value
			window = s.Objectives[0].Window
		}

		r := StatusResult{
			Name:      s.Name,
			UUID:      s.UUID,
			Objective: objective,
			Window:    window,
		}

		m, hasMetrics := metrics[s.UUID]
		if hasMetrics {
			r.SLI = m.SLI
			r.BurnRate = m.BurnRate
			r.SLI1h = m.SLI1h
			r.SLI1d = m.SLI1d
			if m.SLI != nil && objective > 0 {
				budget := ComputeBudget(*m.SLI, objective)
				r.Budget = &budget
			}
		}

		r.Status = ComputeStatus(s, r.SLI, objective)
		results = append(results, r)
	}

	return results
}

// ComputeStatus determines the display status for an SLO.
func ComputeStatus(s Slo, sli *float64, objective float64) string {
	// Lifecycle states take priority.
	if s.ReadOnly != nil && s.ReadOnly.Status != nil {
		switch strings.ToLower(s.ReadOnly.Status.Type) {
		case "creating", "updating", "deleting", "error":
			return strings.Title(s.ReadOnly.Status.Type) //nolint:staticcheck
		}
	}

	if sli == nil {
		return "NODATA"
	}

	if *sli >= objective {
		return "OK"
	}

	return "BREACHING"
}

// ComputeBudget calculates the error budget remaining as a ratio:
// (SLI - objective) / (1 - objective).
func ComputeBudget(sliVal, objective float64) float64 {
	if objective >= 1.0 {
		return 0
	}
	return (sliVal - objective) / (1.0 - objective)
}

// FetchMetrics batch-fetches Prometheus metrics for the given SLOs.
// SLOs are grouped by destination datasource UID to minimize queries.
// Errors are handled gracefully — failed queries result in NODATA.
func FetchMetrics(ctx context.Context, client *prometheus.Client, slos []Slo) map[string]MetricData {
	result := make(map[string]MetricData)

	// Group SLOs by destination datasource UID.
	groups := make(map[string][]Slo)
	for _, s := range slos {
		dsUID := ""
		if s.DestinationDatasource != nil {
			dsUID = s.DestinationDatasource.UID
		}
		groups[dsUID] = append(groups[dsUID], s)
	}

	var mu sync.Mutex

	for dsUID, groupSlos := range groups {
		if dsUID == "" {
			continue // Skip SLOs with no destination datasource.
		}

		uuids := make([]string, len(groupSlos))
		for i, s := range groupSlos {
			uuids[i] = s.UUID
		}
		uuidRegex := strings.Join(uuids, "|")

		type querySpec struct {
			query  string
			setter func(*MetricData, *float64)
		}

		specs := make([]querySpec, 0, 4)
		if q, err := BuildMetricQuery("grafana_slo_sli_window", uuidRegex); err == nil {
			specs = append(specs, querySpec{q, func(m *MetricData, val *float64) { m.SLI = val }})
		}
		if q, err := BuildMetricQuery("grafana_slo_sli_1h", uuidRegex); err == nil {
			specs = append(specs, querySpec{q, func(m *MetricData, val *float64) { m.SLI1h = val }})
		}
		if q, err := BuildMetricQuery("grafana_slo_sli_1d", uuidRegex); err == nil {
			specs = append(specs, querySpec{q, func(m *MetricData, val *float64) { m.SLI1d = val }})
		}
		if q, err := BuildBurnRateQuery(uuidRegex); err == nil {
			specs = append(specs, querySpec{q, func(m *MetricData, val *float64) { m.BurnRate = val }})
		}

		g, gCtx := errgroup.WithContext(ctx)
		for _, spec := range specs {
			g.Go(func() error {
				resp := queryMetric(gCtx, client, dsUID, spec.query)
				if resp == nil {
					return nil
				}
				mu.Lock()
				for _, sample := range resp.Data.Result {
					uuid := sample.Metric[uuidLabel]
					if val := parseSampleValue(sample); val != nil {
						m := result[uuid]
						spec.setter(&m, val)
						result[uuid] = m
					}
				}
				mu.Unlock()
				return nil
			})
		}
		_ = g.Wait() // errors are handled gracefully (NODATA)
	}

	return result
}

// BuildMetricQuery builds a simple label-filtered PromQL query for a metric.
func BuildMetricQuery(metricName, uuidRegex string) (string, error) {
	expr, err := promql.Vector(metricName).
		LabelMatchRegexp(uuidLabel, uuidRegex).
		Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// BuildBurnRateQuery builds a PromQL expression for burn rate.
// Only returns data for ratio-based SLOs that have _rate_5m metrics.
func BuildBurnRateQuery(uuidRegex string) (string, error) {
	successRate := promql.Sum(
		promql.AvgOverTime(
			promql.Vector("grafana_slo_success_rate_5m").
				LabelMatchRegexp(uuidLabel, uuidRegex).Range("1h"),
		),
	).By([]string{uuidLabel})

	totalRate := promql.Sum(
		promql.AvgOverTime(
			promql.Vector("grafana_slo_total_rate_5m").
				LabelMatchRegexp(uuidLabel, uuidRegex).Range("1h"),
		),
	).By([]string{uuidLabel})

	errorRate := promql.Sub(
		promql.N(1),
		promql.ClampMax(promql.Div(successRate, totalRate), 1),
	)

	allowedError := promql.Sub(
		promql.N(1),
		promql.Vector("grafana_slo_objective").
			LabelMatchRegexp(uuidLabel, uuidRegex),
	)

	burnRate := promql.Div(errorRate, allowedError).
		On([]string{uuidLabel})

	expr, err := burnRate.Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// queryMetric executes an instant PromQL query and returns the response.
// Returns nil on error (graceful degradation).
func queryMetric(ctx context.Context, client *prometheus.Client, dsUID, query string) *prometheus.QueryResponse {
	resp, err := client.Query(ctx, dsUID, prometheus.QueryRequest{Query: query})
	if err != nil {
		return nil
	}
	if resp.Status != "success" {
		return nil
	}
	return resp
}

// parseSampleValue extracts the float64 value from an instant query sample.
func parseSampleValue(sample prometheus.Sample) *float64 {
	if len(sample.Value) < 2 {
		return nil
	}

	var val float64
	switch v := sample.Value[1].(type) {
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil
		}
		val = f
	case float64:
		val = v
	default:
		return nil
	}

	if math.IsNaN(val) || math.IsInf(val, 0) {
		return nil
	}

	return &val
}

// ---------------------------------------------------------------------------
// status table codec
// ---------------------------------------------------------------------------

// StatusTableCodec renders StatusResult data as a tabular table.
type StatusTableCodec struct {
	Wide bool
}

// Format returns the codec format identifier.
func (c *StatusTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// Encode writes the status results as a formatted table.
func (c *StatusTableCodec) Encode(w io.Writer, v any) error {
	results, ok := v.([]StatusResult)
	if !ok {
		return errors.New("invalid data type for status table codec: expected []StatusResult")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	if c.Wide {
		fmt.Fprintln(tw, "NAME\tUUID\tOBJECTIVE\tWINDOW\tSLI\tBUDGET\tBURN_RATE\tSLI_1H\tSLI_1D\tSTATUS")
	} else {
		fmt.Fprintln(tw, "NAME\tUUID\tOBJECTIVE\tWINDOW\tSLI\tBUDGET\tSTATUS")
	}

	for _, r := range results {
		objective := formatPercent(r.Objective)
		sliStr := formatOptionalPercent(r.SLI)
		budgetStr := formatOptionalBudget(r.Budget)

		if c.Wide {
			burnRateStr := formatOptionalBurnRate(r.BurnRate)
			sli1h := formatOptionalPercent(r.SLI1h)
			sli1d := formatOptionalPercent(r.SLI1d)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Name, r.UUID, objective, r.Window, sliStr, budgetStr,
				burnRateStr, sli1h, sli1d, r.Status)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Name, r.UUID, objective, r.Window, sliStr, budgetStr, r.Status)
		}
	}

	return tw.Flush()
}

// Decode is not supported for the status table codec.
func (c *StatusTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("status table codec does not support decoding")
}

// ---------------------------------------------------------------------------
// status graph codec
// ---------------------------------------------------------------------------

type statusGraphCodec struct{}

func (c *statusGraphCodec) Format() format.Format {
	return "graph"
}

func (c *statusGraphCodec) Encode(w io.Writer, v any) error {
	results, ok := v.([]StatusResult)
	if !ok {
		return errors.New("invalid data type for status graph codec: expected []StatusResult")
	}

	items := make([]graph.PercentageBarItem, 0, len(results))
	for _, r := range results {
		if r.SLI == nil {
			continue
		}
		items = append(items, graph.PercentageBarItem{
			Name:   r.Name,
			Value:  *r.SLI * 100,
			Target: r.Objective * 100,
		})
	}

	if len(items) == 0 {
		fmt.Fprintln(w, "No metric data available for graph rendering.")
		return nil
	}

	opts := graph.DefaultChartOptions()
	return graph.RenderPercentageBars(w, "SLO Compliance Summary", items, opts)
}

func (c *statusGraphCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("status graph codec does not support decoding")
}

// ---------------------------------------------------------------------------
// formatting helpers
// ---------------------------------------------------------------------------

func formatPercent(v float64) string {
	return fmt.Sprintf("%.2f%%", v*100)
}

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

func formatOptionalBurnRate(v *float64) string {
	if v == nil {
		return "--"
	}
	return fmt.Sprintf("%.2fx", *v)
}
