//nolint:modernize // new(expr) is not valid Go syntax - linter is wrong
package definitions_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/slo/definitions"
)

//nolint:modernize // ptr() creates pointer to value, not pointer to type like new()
func ptr(f float64) *float64 { return &f }

//nolint:modernize // ptr() creates pointer to value, not pointer to type
func TestComputeStatus(t *testing.T) {
	tests := []struct {
		name      string
		slo       definitions.Slo
		sli       *float64
		objective float64
		want      string
	}{
		{
			name:      "OK when SLI meets objective",
			slo:       definitions.Slo{},
			sli:       ptr(0.999),
			objective: 0.995,
			want:      "OK",
		},
		{
			name:      "OK when SLI equals objective",
			slo:       definitions.Slo{},
			sli:       ptr(0.995),
			objective: 0.995,
			want:      "OK",
		},
		{
			name:      "BREACHING when SLI below objective",
			slo:       definitions.Slo{},
			sli:       ptr(0.990),
			objective: 0.995,
			want:      "BREACHING",
		},
		{
			name:      "NODATA when SLI is nil",
			slo:       definitions.Slo{},
			sli:       nil,
			objective: 0.995,
			want:      "NODATA",
		},
		{
			name: "lifecycle status creating",
			slo: definitions.Slo{
				ReadOnly: &definitions.ReadOnly{
					Status: &definitions.Status{Type: "creating"},
				},
			},
			sli:       nil,
			objective: 0.995,
			want:      "Creating",
		},
		{
			name: "lifecycle status error",
			slo: definitions.Slo{
				ReadOnly: &definitions.ReadOnly{
					Status: &definitions.Status{Type: "error"},
				},
			},
			sli:       ptr(0.999),
			objective: 0.995,
			want:      "Error",
		},
		{
			name: "running status does not override metric status",
			slo: definitions.Slo{
				ReadOnly: &definitions.ReadOnly{
					Status: &definitions.Status{Type: "running"},
				},
			},
			sli:       ptr(0.999),
			objective: 0.995,
			want:      "OK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := definitions.ComputeStatus(tt.slo, tt.sli, tt.objective)
			if got != tt.want {
				t.Errorf("ComputeStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComputeBudget(t *testing.T) {
	tests := []struct {
		name      string
		sli       float64
		objective float64
		want      float64
	}{
		{
			name:      "positive budget",
			sli:       0.9972,
			objective: 0.995,
			want:      0.44, // (0.9972 - 0.995) / (1 - 0.995) = 0.0022 / 0.005 = 0.44
		},
		{
			name:      "negative budget (breaching)",
			sli:       0.9900,
			objective: 0.995,
			want:      -1.0, // (0.990 - 0.995) / (1 - 0.995) = -0.005 / 0.005 = -1.0
		},
		{
			name:      "zero budget (at objective)",
			sli:       0.995,
			objective: 0.995,
			want:      0.0,
		},
		{
			name:      "100% SLI",
			sli:       1.0,
			objective: 0.995,
			want:      1.0,
		},
		{
			name:      "objective at 100%",
			sli:       0.999,
			objective: 1.0,
			want:      0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := definitions.ComputeBudget(tt.sli, tt.objective)
			// Use tolerance for floating point comparison.
			diff := got - tt.want
			if diff < -0.001 || diff > 0.001 {
				t.Errorf("ComputeBudget(%v, %v) = %v, want %v", tt.sli, tt.objective, got, tt.want)
			}
		})
	}
}

func TestStatusTableCodec_InvalidType(t *testing.T) {
	codec := definitions.StatusTable().Codec("table")
	var buf bytes.Buffer
	err := codec.Encode(&buf, "invalid")
	if err == nil {
		t.Error("expected error for invalid data type")
	}
}

//nolint:modernize // ptr() creates pointer to value, not pointer to type
func TestBuildStatusResults(t *testing.T) {
	slos := []definitions.Slo{
		{
			UUID: "uuid-1",
			Name: "test-slo",
			Objectives: []definitions.Objective{
				{Value: 0.995, Window: "28d"},
			},
		},
	}

	burnRate := 2.0
	metrics := map[string]definitions.MetricData{
		"uuid-1": {SLI: ptr(0.9972), BurnRate: &burnRate},
	}

	results := definitions.BuildStatusResults(slos, metrics)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Name != "test-slo" {
		t.Errorf("expected name test-slo, got %s", r.Name)
	}
	if r.Status != "OK" {
		t.Errorf("expected status OK, got %s", r.Status)
	}
	if r.Budget == nil {
		t.Fatal("expected budget to be computed")
	}
	// Budget = (0.9972 - 0.995) / (1 - 0.995) = 0.44.
	diff := *r.Budget - 0.44
	if diff < -0.001 || diff > 0.001 {
		t.Errorf("expected budget ~0.44, got %v", *r.Budget)
	}
	if r.BurnRate == nil {
		t.Fatal("expected burn rate to be populated from metric data")
	}
	if *r.BurnRate != 2.0 {
		t.Errorf("expected burn rate 2.0, got %v", *r.BurnRate)
	}
}
