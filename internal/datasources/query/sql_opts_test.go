package query_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLQueryOptsResolveExpr(t *testing.T) {
	queryFile := filepath.Join(t.TempDir(), "query.sql")
	const fileSQL = "SELECT 1\n-- preserve this newline\n"
	require.NoError(t, os.WriteFile(queryFile, []byte(fileSQL), 0o600))

	tests := []struct {
		name             string
		expr             string
		queryFile        string
		queryFileChanged bool
		args             []string
		stdin            string
		want             string
		wantErr          string
	}{
		{
			name: "positional argument",
			args: []string{"SELECT 1"},
			want: "SELECT 1",
		},
		{
			name: "expr flag",
			expr: "SELECT 1",
			want: "SELECT 1",
		},
		{
			name:      "file preserves bytes",
			queryFile: queryFile,
			want:      fileSQL,
		},
		{
			name:      "stdin",
			queryFile: "-",
			stdin:     "SELECT 2\n",
			want:      "SELECT 2\n",
		},
		{
			name:    "no source",
			wantErr: "expression is required",
		},
		{
			name:    "positional and expr",
			expr:    "SELECT 1",
			args:    []string{"SELECT 2"},
			wantErr: "not both",
		},
		{
			name:      "positional and file",
			queryFile: queryFile,
			args:      []string{"SELECT 2"},
			wantErr:   "not multiple sources",
		},
		{
			name:      "expr and file",
			expr:      "SELECT 1",
			queryFile: queryFile,
			wantErr:   "not multiple sources",
		},
		{
			name:             "explicit empty file path",
			queryFileChanged: true,
			wantErr:          "requires a file path",
		},
		{
			name:      "missing file",
			queryFile: filepath.Join(t.TempDir(), "missing.sql"),
			wantErr:   "failed to read --query-file",
		},
		{
			name:      "empty file",
			queryFile: emptyQueryFile(t),
			wantErr:   "is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &dsquery.SQLQueryOpts{SharedOpts: dsquery.SharedOpts{Expr: tt.expr}, QueryFile: tt.queryFile}
			got, err := opts.ResolveExpr(tt.args, 0, strings.NewReader(tt.stdin), tt.queryFileChanged)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSQLQueryOptsSetup_DoesNotLeakIntoSharedOpts(t *testing.T) {
	shared := &dsquery.SharedOpts{}
	sharedFlags := newTestFlagSet()
	shared.Setup(sharedFlags, false)
	assert.Nil(t, sharedFlags.Lookup("query-file"))

	sql := &dsquery.SQLQueryOpts{}
	sqlFlags := newTestFlagSet()
	sql.Setup(sqlFlags, false)
	require.NotNil(t, sqlFlags.Lookup("query-file"))
	require.NoError(t, sqlFlags.Parse([]string{"--query-file", "query.sql"}))
	assert.Equal(t, "query.sql", sql.QueryFile)
}

func newTestFlagSet() *pflag.FlagSet {
	return pflag.NewFlagSet("test", pflag.ContinueOnError)
}

func emptyQueryFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "empty.sql")
	require.NoError(t, os.WriteFile(path, []byte(" \n\t"), 0o600))
	return path
}
