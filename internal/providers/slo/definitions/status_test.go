//nolint:modernize // new(expr) is not valid Go syntax - linter is wrong
package definitions_test

import (
	"bytes"
	"strings"
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

//nolint:modernize // ptr() creates pointer to value, not pointer to type
func TestStatusTableCodec_Encode(t *testing.T) {
	results := []definitions.StatusResult{
		{
			Name:      "payment-api-latency",
			UUID:      "abc123def",
			Objective: 0.995,
			Window:    "28d",
			SLI:       ptr(0.9972),
			Budget:    ptr(0.44),
			Status:    "OK",
		},
		{
			Name:      "checkout-availability",
			UUID:      "xyz789ghi",
			Objective: 0.999,
			Window:    "28d",
			SLI:       ptr(0.9985),
			Budget:    ptr(-0.50),
			Status:    "BREACHING",
		},
		{
			Name:      "new-feature-slo",
			UUID:      "pqr012stu",
			Objective: 0.995,
			Window:    "28d",
			SLI:       nil,
			Budget:    nil,
			Status:    "Creating",
		},
	}

	t.Run("default table", func(t *testing.T) {
		codec := &definitions.StatusTableCodec{}
		var buf bytes.Buffer
		err := codec.Encode(&buf, results)
		if err != nil {
			t.Fatalf("Encode() error = %v", err)
		}

		output := buf.String()

		// Verify header.
		if !strings.Contains(output, "NAME") || !strings.Contains(output, "UUID") ||
			!strings.Contains(output, "OBJECTIVE") || !strings.Contains(output, "STATUS") {
			t.Errorf("missing expected header columns in:\n%s", output)
		}

		// Verify data rows.
		if !strings.Contains(output, "payment-api-latency") {
			t.Errorf("missing payment-api-latency in:\n%s", output)
		}
		if !strings.Contains(output, "99.50%") {
			t.Errorf("missing objective 99.50%% in:\n%s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("missing OK status in:\n%s", output)
		}
		if !strings.Contains(output, "BREACHING") {
			t.Errorf("missing BREACHING status in:\n%s", output)
		}
		if !strings.Contains(output, "Creating") {
			t.Errorf("missing Creating status in:\n%s", output)
		}
		if !strings.Contains(output, "--") {
			t.Errorf("missing -- for NODATA values in:\n%s", output)
		}

		// Default table should NOT have wide columns.
		if strings.Contains(output, "SLI_1H") {
			t.Errorf("default table should not have SLI_1H column:\n%s", output)
		}
	})

	t.Run("wide table", func(t *testing.T) {
		burnRate := 1.5
		wideResults := []definitions.StatusResult{
			{
				Name:      "payment-api-latency",
				UUID:      "abc123def",
				Objective: 0.995,
				Window:    "28d",
				SLI:       ptr(0.9972),
				Budget:    ptr(0.44),
				BurnRate:  &burnRate,
				SLI1h:     ptr(0.9991),
				SLI1d:     ptr(0.9980),
				Status:    "OK",
			},
		}

		codec := &definitions.StatusTableCodec{Wide: true}
		var buf bytes.Buffer
		err := codec.Encode(&buf, wideResults)
		if err != nil {
			t.Fatalf("Encode() error = %v", err)
		}

		output := buf.String()

		// Verify wide-specific columns.
		if !strings.Contains(output, "BURN_RATE") {
			t.Errorf("wide table should have BURN_RATE column:\n%s", output)
		}
		if !strings.Contains(output, "SLI_1H") {
			t.Errorf("wide table should have SLI_1H column:\n%s", output)
		}
		if !strings.Contains(output, "SLI_1D") {
			t.Errorf("wide table should have SLI_1D column:\n%s", output)
		}
		if !strings.Contains(output, "1.50x") {
			t.Errorf("wide table should show burn rate 1.50x:\n%s", output)
		}
	})
}

func TestStatusTableCodec_InvalidType(t *testing.T) {
	codec := &definitions.StatusTableCodec{}
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
