package root_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLFilenameCommands(t *testing.T) {
	for _, ds := range []struct{ kind, plugin string }{
		{"mysql", "mysql"}, {"postgres", "grafana-postgresql-datasource"},
		{"clickhouse", "grafana-clickhouse-datasource"}, {"athena", "grafana-athena-datasource"},
		{"mssql", "mssql"}, {"bigquery", "grafana-bigquery-datasource"}, {"pinot", "startree-pinot-datasource"},
	} {
		for _, source := range []string{"file", "stdin", "inline"} {
			t.Run(ds.kind+"/"+source, func(t *testing.T) {
				testutils.SandboxConfigEnv(t)
				const sql = "-- daily report\r\nSELECT 1 FROM events;\r\n"
				queries := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch {
					case r.URL.Path == "/api/datasources/uid/test-ds":
						_, err := fmt.Fprintf(w, `{"uid":"test-ds","type":%q}`, ds.plugin)
						assert.NoError(t, err)
					case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/query"):
						queries++
						var body struct {
							Queries []map[string]any `json:"queries"`
						}
						if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) || !assert.Len(t, body.Queries, 1) {
							return
						}
						field := "rawSql"
						if ds.kind == "pinot" {
							field = "pinotQlCode"
						}
						got, ok := body.Queries[0][field].(string)
						assert.True(t, ok)
						if source == "inline" {
							if ds.kind == "mssql" {
								assert.Contains(t, got, "TOP")
							} else {
								assert.Contains(t, got, "LIMIT")
							}
						} else {
							assert.Equal(t, sql, got, "file input must execute verbatim")
						}
						_, err := fmt.Fprint(w, `{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"n","type":"number"}]},"data":{"values":[[1]]}}]}}}`)
						assert.NoError(t, err)
					default:
						http.NotFound(w, r)
					}
				}))
				defer server.Close()
				config := testutils.CreateTempFile(t, fmt.Sprintf("version: 1\nstacks:\n  test:\n    grafana:\n      server: %s\n      token: test-token\n      org-id: 1\ncontexts:\n  test:\n    stack: test\ncurrent-context: test\n", server.URL))
				cmd := root.Command("test")
				var stdout, stderr bytes.Buffer
				cmd.SetOut(&stdout)
				cmd.SetErr(&stderr)
				args := []string{"datasources", ds.kind, "query", "-d", "test-ds", "--config", config, "-o", "json"}
				switch source {
				case "file":
					file := filepath.Join(t.TempDir(), "report.sql")
					require.NoError(t, os.WriteFile(file, []byte(sql), 0o600))
					args = append(args, "--filename", file)
				case "stdin":
					cmd.SetIn(strings.NewReader(sql))
					args = append(args, "-f", "-")
				case "inline":
					args = append(args, "SELECT 1 FROM events")
				}
				cmd.SetArgs(args)
				require.NoError(t, cmd.Execute())
				assert.Equal(t, 1, queries)
				assert.True(t, json.Valid(stdout.Bytes()), stdout.String())
				if source != "inline" {
					assert.Contains(t, stderr.String(), "without the default row limit")
				}
			})
		}
	}
}

func TestSQLFilenameValidationBeforeIO(t *testing.T) {
	for _, kind := range []string{"mysql", "postgres", "clickhouse", "athena", "mssql", "bigquery", "pinot"} {
		for _, flags := range [][]string{
			{"-f", "-", "--limit", "0"}, {"-f", "-", "--limit", "100"},
			{"-f", "-", "--limit=-1"}, {"--limit", "-1", "--filename", "missing.sql"},
			{"--filename="}, {"-f", "-", "--expr", "SELECT 1"},
			{"-f", "-", "SELECT 1"}, {"-f", "-"},
		} {
			t.Run(kind+"/"+strings.Join(flags, " "), func(t *testing.T) {
				testutils.SandboxConfigEnv(t)
				cmd := root.Command("test")
				var stdout, stderr bytes.Buffer
				cmd.SetOut(&stdout)
				cmd.SetErr(&stderr)
				input := ""
				for _, flag := range flags {
					if strings.HasPrefix(flag, "--limit") {
						input = "SELECT 1"
						break
					}
				}
				stdin := strings.NewReader(input)
				cmd.SetIn(stdin)
				cmd.SetArgs(append([]string{"datasources", kind, "query"}, flags...))
				var usage *gcxerrors.UsageError
				err := cmd.Execute()
				require.ErrorAs(t, err, &usage)
				for _, flag := range flags {
					if strings.HasPrefix(flag, "--limit") {
						assert.ErrorContains(t, err, "--filename cannot be combined with --limit")
						assert.NotContains(t, stderr.String(), "Reading SQL from stdin")
						assert.Equal(t, len(input), stdin.Len(), "input conflicts must fail before reading stdin")
						break
					}
				}
			})
		}
	}
}
