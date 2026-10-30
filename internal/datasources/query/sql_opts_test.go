package query_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLQueryOptsResolveExpr(t *testing.T) {
	file := filepath.Join(t.TempDir(), "query.sql")
	empty := filepath.Join(t.TempDir(), "empty.sql")
	const sql = "-- report\r\nSELECT 1;\r\n"
	require.NoError(t, os.WriteFile(file, []byte(sql), 0o600))
	require.NoError(t, os.WriteFile(empty, []byte(" \r\n\t"), 0o600))

	for _, tt := range []struct {
		name      string
		flags     []string
		stdin     string
		want      string
		wantErr   string
		fileInput bool
	}{
		{name: "positional", flags: []string{"SELECT 1"}, want: "SELECT 1"},
		{name: "expression", flags: []string{"--expr", "SELECT 2"}, want: "SELECT 2"},
		{name: "file bytes", flags: []string{"--filename", file}, want: sql, fileInput: true},
		{name: "stdin bytes", flags: []string{"-f", "-"}, stdin: sql, want: sql, fileInput: true},
		{name: "missing source", wantErr: "expression is required"},
		{name: "two inline sources", flags: []string{"--expr", "SELECT 1", "SELECT 2"}, wantErr: "not both"},
		{name: "file and positional", flags: []string{"-f", file, "SELECT 1"}, wantErr: "not multiple sources"},
		{name: "file and expr", flags: []string{"-f", file, "--expr", "SELECT 1"}, wantErr: "not multiple sources"},
		{name: "file and empty expr", flags: []string{"-f", file, "--expr="}, wantErr: "not multiple sources"},
		{name: "empty filename", flags: []string{"--filename="}, wantErr: "requires a file path"},
		{name: "missing file", flags: []string{"-f", file + ".missing"}, wantErr: "failed to read --filename"},
		{name: "empty file", flags: []string{"-f", empty}, wantErr: "is empty"},
		{name: "empty stdin", flags: []string{"-f", "-"}, wantErr: "is empty"},
		{name: "limit", flags: []string{"-f", file, "--limit", "100"}, wantErr: "cannot be combined with --limit"},
		{name: "unlimited", flags: []string{"-f", "-", "--limit", "0"}, wantErr: "cannot be combined with --limit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := &dsquery.SQLQueryOpts{}
			cmd := &cobra.Command{Use: "query"}
			opts.Setup(cmd.Flags(), false)
			cmd.Flags().Int("limit", 100, "")
			require.NoError(t, cmd.ParseFlags(tt.flags))
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)
			cmd.SetIn(strings.NewReader(tt.stdin))
			got, err := opts.ResolveExpr(cmd, cmd.Flags().Args(), 0)
			if tt.wantErr != "" {
				var usage *gcxerrors.UsageError
				require.ErrorAs(t, err, &usage)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			if tt.fileInput {
				assert.Contains(t, stderr.String(), "without the default row limit")
				assert.Zero(t, opts.RowLimit(100))
			} else {
				assert.Empty(t, stderr.String())
				assert.Equal(t, 100, opts.RowLimit(100))
			}
			if tt.stdin != "" {
				assert.Contains(t, stderr.String(), "Reading SQL from stdin")
			}
		})
	}
}

func TestSQLQueryOptsSetupDoesNotLeakIntoSharedOpts(t *testing.T) {
	shared := &dsquery.SharedOpts{}
	flags := pflag.NewFlagSet("shared", pflag.ContinueOnError)
	shared.Setup(flags, false)
	assert.Nil(t, flags.Lookup("filename"))

	sql := &dsquery.SQLQueryOpts{}
	sqlFlags := pflag.NewFlagSet("sql", pflag.ContinueOnError)
	sql.Setup(sqlFlags, false)
	require.NotNil(t, sqlFlags.Lookup("filename"))
	require.NoError(t, sqlFlags.Parse([]string{"-f", "query.sql"}))
	assert.Equal(t, "query.sql", sql.Filename)
}
