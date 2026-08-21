package elasticsearch_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/elasticsearch"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryCmd_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A zero-value loader has no context/config wired up, so any code
			// path that reaches config loading or datasource resolution fails
			// with an unrelated error. Asserting on the specific validation
			// message proves validation ran first.
			loader := &providers.ConfigLoader{}
			cmd := elasticsearch.QueryCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
