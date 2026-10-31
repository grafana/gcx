package mysql_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/datasources/mysql"
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
			name:    "negative limit rejected before any config/datasource I/O",
			args:    []string{"--limit=-5", "SELECT 1"},
			wantErr: "--limit must be >= 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A zero-value loader has no context/config wired up, so any code
			// path that reaches config loading or datasource resolution fails
			// with an unrelated error. Asserting on the specific validation
			// message proves validation ran first.
			loader := &providers.ConfigLoader{}
			cmd := mysql.QueryCmd(loader)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestQueryCmd_QueryFileValidationBeforeIO(t *testing.T) {
	emptyFile := filepath.Join(t.TempDir(), "empty.sql")
	require.NoError(t, os.WriteFile(emptyFile, []byte(" \n\t"), 0o600))

	tests := []struct {
		name      string
		queryFile string
		wantErr   string
	}{
		{
			name:      "missing file",
			queryFile: filepath.Join(t.TempDir(), "missing.sql"),
			wantErr:   "failed to read --query-file",
		},
		{
			name:      "empty file",
			queryFile: emptyFile,
			wantErr:   "is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A zero-value loader has no context/config wired up. Seeing the
			// query-file error proves input validation completed before config or
			// datasource I/O.
			loader := &providers.ConfigLoader{}
			cmd := mysql.QueryCmd(loader)
			cmd.SetArgs([]string{"--query-file", tt.queryFile})
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
