package definitions_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/slo/definitions"
)

func TestBuildMetricQuery(t *testing.T) {
	q, err := definitions.BuildMetricQuery("grafana_slo_sli_window", "uuid-1|uuid-2")
	if err != nil {
		t.Fatalf("BuildMetricQuery() error = %v", err)
	}

	for _, want := range []string{"grafana_slo_sli_window", "grafana_slo_uuid", "uuid-1|uuid-2"} {
		if !strings.Contains(q, want) {
			t.Errorf("expected %q in query, got: %s", want, q)
		}
	}
}

func TestBuildBurnRateQuery(t *testing.T) {
	q, err := definitions.BuildBurnRateQuery("uuid-1|uuid-2")
	if err != nil {
		t.Fatalf("BuildBurnRateQuery() error = %v", err)
	}

	for _, want := range []string{
		"grafana_slo_success_rate_5m",
		"grafana_slo_total_rate_5m",
		"grafana_slo_objective",
		"grafana_slo_uuid",
		"uuid-1|uuid-2",
		"avg_over_time",
		"clamp_max",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("expected %q in burn rate query, got: %s", want, q)
		}
	}
}
