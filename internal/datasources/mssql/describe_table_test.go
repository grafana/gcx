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
			name:    "conflicting schema in both the table name and --schema rejected",
			args:    []string{"dbo.WORLD_DATA", "--schema", "sales"},
			wantErr: "not both",
		},
		{
			name:    "empty table name rejected instead of matching every row silently",
			args:    []string{""},
			wantErr: "table name is required",
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

// TestDescribeTableCmd_AgreeingSchemaNotRejected pins the false-positive fix:
// a schema-qualified table name and an equal --schema value describe the same
// table and must not be treated as a conflict, even though a zero-value
// loader means the command still errors downstream on config/datasource I/O.
func TestDescribeTableCmd_AgreeingSchemaNotRejected(t *testing.T) {
	loader := &providers.ConfigLoader{}
	cmd := mssql.DescribeTableCmd(loader)
	cmd.SetArgs([]string{"dbo.WORLD_DATA", "--schema", "dbo"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "not both")
}
