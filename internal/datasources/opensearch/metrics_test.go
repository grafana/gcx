package opensearch_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/opensearch"
)

// metrics.go's own validation (--group-size, --agg/--field) had no coverage
// on either sibling (elasticsearch's metrics.go has no metrics_test.go
// either) — new code still owes the coverage its own contract requires.
func TestMetricsCmd_ValidationErrors(t *testing.T) {
	runValidationCases(t, opensearch.MetricsCmd, []validationCase{
		{
			name:    "--group-size 0 rejected before any config/datasource I/O",
			args:    []string{"--group-size", "0"},
			wantErr: "--group-size must be at least 1, got 0",
		},
		{
			name:    "negative --group-size rejected before any config/datasource I/O",
			args:    []string{"--group-size", "-1"},
			wantErr: "--group-size must be at least 1, got -1",
		},
		{
			name:    "unknown --agg rejected before any config/datasource I/O",
			args:    []string{"--agg", "percentiles"},
			wantErr: "supported: avg, cardinality, count, max, min, sum",
		},
		{
			name:    "--agg avg without --field rejected before any config/datasource I/O",
			args:    []string{"--agg", "avg"},
			wantErr: "--field is required for --agg avg",
		},
	})
}
