package postgres_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/postgres"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribeTableCmd_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "empty --schema rejected instead of silently matching every schema",
			args:    []string{"orders", "--schema="},
			wantErr: "--schema must not be empty",
		},
		{
			name:    "constraint metadata requires explicit schema before config I/O",
			args:    []string{"orders", "--include-constraints", "-o", "json"},
			wantErr: "--include-constraints requires an explicit schema",
		},
		{
			name:    "constraint metadata rejects table output before config I/O",
			args:    []string{"public.orders", "--include-constraints"},
			wantErr: "requires JSON, YAML, or agent output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A zero-value loader has no context/config wired up, so any code
			// path that reaches config loading or datasource resolution fails
			// with an unrelated error. Asserting on the specific validation
			// message proves validation ran first.
			loader := &providers.ConfigLoader{}
			cmd := postgres.DescribeTableCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
