package mssql_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/mssql"
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
			name:    "empty --schema rejected instead of silently searching every schema",
			args:    []string{"WORLD_DATA", "--schema="},
			wantErr: "--schema must not be empty",
		},
		{
			name:    "schema in both the table name and --schema rejected",
			args:    []string{"dbo.WORLD_DATA", "--schema", "dbo"},
			wantErr: "not both",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A zero-value loader has no context/config wired up, so any code
			// path that reaches config loading or datasource resolution fails
			// with an unrelated error. Asserting on the specific validation
			// message proves validation ran first.
			loader := &providers.ConfigLoader{}
			cmd := mssql.DescribeTableCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
