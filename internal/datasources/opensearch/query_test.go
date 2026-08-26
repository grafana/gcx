package opensearch_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/opensearch"
)

func TestQueryCmd_ValidationErrors(t *testing.T) {
	runValidationCases(t, opensearch.QueryCmd, []validationCase{
		{
			name:    "unknown --mode rejected before any config/datasource I/O",
			args:    []string{"--mode", "bogus"},
			wantErr: `--mode must be "documents" or "logs", got "bogus"`,
		},
		{
			name:    "--limit 0 rejected before any config/datasource I/O",
			args:    []string{"--limit", "0"},
			wantErr: "--limit must be between 1 and 1000, got 0",
		},
		{
			name:    "--limit above max rejected before any config/datasource I/O",
			args:    []string{"--limit", "5000"},
			wantErr: "--limit must be between 1 and 1000, got 5000",
		},
		{
			name:    "negative --limit rejected before any config/datasource I/O",
			args:    []string{"--limit", "-1"},
			wantErr: "--limit must be between 1 and 1000, got -1",
		},
	})
}
