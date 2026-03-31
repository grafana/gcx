//nolint:modernize // new(expr) is not valid Go syntax - linter is wrong
package reports_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/grafana/gcx/internal/providers/slo/reports"
)

//nolint:modernize // ptr() creates pointer to value, not pointer to type like new()
func ptr(f float64) *float64 { return &f }

//nolint:modernize // ptr() creates pointer to value, not pointer to type
func TestBuildReportStatusResults(t *testing.T) {
	rpts := []reports.Report{
		{
			UUID:     "rpt-1",
			Name:     "Weekly Platform Report",
			TimeSpan: "weeklySundayToSunday",
			ReportDefinition: reports.ReportDefinition{
				Slos: []reports.ReportSlo{
					{SloUUID: "slo-1"},
					{SloUUID: "slo-2"},
				},
			},
		},
	}

	sloIndex := map[string]definitions.Slo{
		"slo-1": {UUID: "slo-1", Name: "payment-api-latency", Objectives: []definitions.Objective{{Value: 0.995, Window: "28d"}}},
		"slo-2": {UUID: "slo-2", Name: "checkout-avail", Objectives: []definitions.Objective{{Value: 0.999, Window: "28d"}}},
	}

	sloResultIndex := map[string]definitions.StatusResult{
		"slo-1": {Name: "payment-api-latency", UUID: "slo-1", Objective: 0.995, Window: "28d", SLI: ptr(0.9972), Budget: ptr(0.44), Status: "OK"},
		"slo-2": {Name: "checkout-avail", UUID: "slo-2", Objective: 0.999, Window: "28d", SLI: ptr(0.9985), Budget: ptr(-0.50), Status: "BREACHING"},
	}

	results := reports.BuildReportStatusResults(rpts, sloIndex, sloResultIndex)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Name != "Weekly Platform Report" {
		t.Errorf("expected name 'Weekly Platform Report', got %s", r.Name)
	}
	if r.TimeSpan != "weekly" {
		t.Errorf("expected timeSpan 'weekly', got %s", r.TimeSpan)
	}
	if r.SLOCount != 2 {
		t.Errorf("expected 2 SLOs, got %d", r.SLOCount)
	}
	if r.CombinedSLI == nil {
		t.Fatal("expected combined SLI to be computed")
	}
	// Combined SLI = (0.9972 + 0.9985) / 2 = 0.99785
	wantSLI := 0.99785
	if diff := *r.CombinedSLI - wantSLI; diff < -0.0001 || diff > 0.0001 {
		t.Errorf("expected combined SLI ~%.5f, got %.5f", wantSLI, *r.CombinedSLI)
	}
	if r.CombinedBudget == nil {
		t.Fatal("expected combined budget to be computed")
	}
	if r.Status != "OK" {
		t.Errorf("expected status OK, got %s", r.Status)
	}
	if len(r.SLOs) != 2 {
		t.Errorf("expected 2 SLO details, got %d", len(r.SLOs))
	}
}

func TestBuildReportStatusResults_NoData(t *testing.T) {
	rpts := []reports.Report{
		{
			UUID:     "rpt-1",
			Name:     "Empty Report",
			TimeSpan: "calendarMonth",
			ReportDefinition: reports.ReportDefinition{
				Slos: []reports.ReportSlo{
					{SloUUID: "slo-missing"},
				},
			},
		},
	}

	results := reports.BuildReportStatusResults(rpts, nil, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.CombinedSLI != nil {
		t.Errorf("expected nil combined SLI, got %v", *r.CombinedSLI)
	}
	if r.Status != "NODATA" {
		t.Errorf("expected NODATA status, got %s", r.Status)
	}
}

//nolint:modernize // ptr() creates pointer to value, not pointer to type
func TestBuildReportStatusResults_LifecycleStatus(t *testing.T) {
	rpts := []reports.Report{
		{
			UUID:     "rpt-1",
			Name:     "Report with Creating SLO",
			TimeSpan: "calendarYear",
			ReportDefinition: reports.ReportDefinition{
				Slos: []reports.ReportSlo{
					{SloUUID: "slo-1"},
					{SloUUID: "slo-2"},
				},
			},
		},
	}

	sloIndex := map[string]definitions.Slo{
		"slo-1": {UUID: "slo-1", Name: "healthy-slo", Objectives: []definitions.Objective{{Value: 0.995, Window: "28d"}}},
		"slo-2": {
			UUID: "slo-2", Name: "creating-slo",
			ReadOnly: &definitions.ReadOnly{Status: &definitions.Status{Type: "creating"}},
		},
	}

	sloResultIndex := map[string]definitions.StatusResult{
		"slo-1": {Name: "healthy-slo", UUID: "slo-1", Objective: 0.995, SLI: ptr(0.999), Status: "OK"},
		"slo-2": {Name: "creating-slo", UUID: "slo-2", Status: "Creating"},
	}

	results := reports.BuildReportStatusResults(rpts, sloIndex, sloResultIndex)

	if results[0].Status != "Creating" {
		t.Errorf("expected Creating status, got %s", results[0].Status)
	}
}

//nolint:modernize // ptr() creates pointer to value, not pointer to type
func TestReportStatusTableCodec_Encode(t *testing.T) {
	results := []reports.ReportStatusResult{
		{
			Name:           "Weekly Platform Report",
			UUID:           "rpt-1",
			TimeSpan:       "weekly",
			SLOCount:       3,
			CombinedSLI:    ptr(0.9982),
			CombinedBudget: ptr(0.36),
			Status:         "OK",
			SLOs: []definitions.StatusResult{
				{Name: "payment-api-latency", SLI: ptr(0.9972), Budget: ptr(0.44), Status: "OK"},
				{Name: "checkout-avail", SLI: ptr(0.9985), Budget: ptr(-0.50), Status: "BREACHING"},
			},
		},
	}

	t.Run("default table", func(t *testing.T) {
		codec := &reports.ReportStatusTableCodec{}
		var buf bytes.Buffer
		err := codec.Encode(&buf, results)
		if err != nil {
			t.Fatalf("Encode() error = %v", err)
		}

		output := buf.String()

		for _, want := range []string{"NAME", "TIME_SPAN", "SLOS", "COMBINED_SLI", "COMBINED_BUDGET", "STATUS"} {
			if !strings.Contains(output, want) {
				t.Errorf("missing header %q in:\n%s", want, output)
			}
		}

		if !strings.Contains(output, "Weekly Platform Report") {
			t.Errorf("missing report name in:\n%s", output)
		}
		if !strings.Contains(output, "weekly") {
			t.Errorf("missing time span in:\n%s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("missing OK status in:\n%s", output)
		}

		// Default should NOT show per-SLO rows.
		if strings.Contains(output, "payment-api-latency") {
			t.Errorf("default table should not show per-SLO rows:\n%s", output)
		}
	})

	t.Run("wide table", func(t *testing.T) {
		codec := &reports.ReportStatusTableCodec{Wide: true}
		var buf bytes.Buffer
		err := codec.Encode(&buf, results)
		if err != nil {
			t.Fatalf("Encode() error = %v", err)
		}

		output := buf.String()

		// Wide should show per-SLO rows.
		if !strings.Contains(output, "payment-api-latency") {
			t.Errorf("wide table should show per-SLO rows:\n%s", output)
		}
		if !strings.Contains(output, "checkout-avail") {
			t.Errorf("wide table should show per-SLO rows:\n%s", output)
		}
		if !strings.Contains(output, "BREACHING") {
			t.Errorf("wide table should show per-SLO statuses:\n%s", output)
		}
	})
}

func TestReportStatusTableCodec_InvalidType(t *testing.T) {
	codec := &reports.ReportStatusTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "invalid")
	if err == nil {
		t.Error("expected error for invalid data type")
	}
}
