package pinot_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/pinot"
	"github.com/grafana/gcx/internal/providers"
	querypinot "github.com/grafana/gcx/internal/query/pinot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryCmd_LimitFlagUsesSharedUsage(t *testing.T) {
	cmd := pinot.QueryCmd(&providers.ConfigLoader{})
	usage := cmd.Flags().Lookup("limit").Usage
	assert.Equal(t, querypinot.LimitFlagUsage(querypinot.MaxLimit), usage)
}

func TestQueryCmd_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "negative limit rejected before any config/datasource I/O",
			args:    []string{"--limit=-5", "SELECT 1"},
			wantErr: "--limit must be >= 0",
		},
		{
			name:    "missing table rejected before any config/datasource I/O",
			args:    []string{"SELECT 1"},
			wantErr: "could not derive a table name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A zero-value loader has no context/config wired up, so any code
			// path that reaches config loading or datasource resolution fails
			// with an unrelated error. Asserting on the specific validation
			// message proves validation ran first.
			loader := &providers.ConfigLoader{}
			cmd := pinot.QueryCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
