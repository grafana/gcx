package cloudmonitoring_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/cloudmonitoring"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListMetricsCmd_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "whitespace-only --project rejected instead of round-tripping to Google",
			args:    []string{"--project", "   "},
			wantErr: "--project is required",
		},
		{
			name:    "explicit empty --service rejected instead of building an unmatchable filter",
			args:    []string{"--project", "p", "--service="},
			wantErr: "--service must not be empty",
		},
		{
			name:    "whitespace-only --service rejected instead of building an unmatchable filter",
			args:    []string{"--project", "p", "--service", "   "},
			wantErr: "--service must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A zero-value loader has no context/config wired up, so any code
			// path that reaches config loading or datasource resolution fails
			// with an unrelated error. Asserting on the specific validation
			// message proves validation ran first.
			loader := &providers.ConfigLoader{}
			cmd := cloudmonitoring.ListMetricsCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
