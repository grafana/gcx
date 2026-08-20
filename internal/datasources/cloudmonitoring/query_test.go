package cloudmonitoring_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/cloudmonitoring"
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
			name:    "unknown --reducer rejected before any config/datasource I/O",
			args:    []string{"--project", "p", "--metric", "m", "--reducer", "REDUCE_AVERAGE"},
			wantErr: `--reducer "REDUCE_AVERAGE" is not a valid cross-series reducer`,
		},
		{
			name:    "unknown --aligner rejected before any config/datasource I/O",
			args:    []string{"--project", "p", "--metric", "m", "--aligner", "ALIGN_AVERAGE"},
			wantErr: `--aligner "ALIGN_AVERAGE" is not a valid per-series aligner`,
		},
		{
			name:    "CloudWatch-style --alignment-period rejected before any config/datasource I/O",
			args:    []string{"--project", "p", "--metric", "m", "--alignment-period", "60s"},
			wantErr: "--alignment-period",
		},
		{
			name:    "whitespace-only --group-by entry rejected before any config/datasource I/O",
			args:    []string{"--project", "p", "--metric", "m", "--group-by", "   "},
			wantErr: "--group-by entries must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A zero-value loader has no context/config wired up, so any code
			// path that reaches config loading or datasource resolution fails
			// with an unrelated error. Asserting on the specific validation
			// message proves validation ran first.
			loader := &providers.ConfigLoader{}
			cmd := cloudmonitoring.QueryCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
