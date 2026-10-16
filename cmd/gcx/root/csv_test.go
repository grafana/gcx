package root_test

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVQueryCommands(t *testing.T) {
	metricFrame := `{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"Time","type":"time"},{"name":"Value","type":"number","labels":{"job":"api"}}]},"data":{"values":[[1700000000125],[3.5]]}}]}}}`
	logFrame := `{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"Time","type":"time"},{"name":"Line","type":"string","labels":{"app":"api"}}]},"data":{"values":[[1700000000125],["original, line"]]}}]}}}`
	tableFrame := `{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"database","type":"string"},{"name":"name","type":"string"},{"name":"engine","type":"string"},{"name":"total_rows","type":"number"},{"name":"total_bytes","type":"number"}]},"data":{"values":[["otel"],["logs"],["MergeTree"],[42],[1024]]}}]}}}`
	for _, tt := range []struct{ path, kind, expr, response, want string }{
		{"metrics query", "prometheus", "up", metricFrame, "1700000000.125"},
		{"datasources clickhouse list-tables", "clickhouse", "", tableFrame, "database,name,engine,total_rows,total_bytes\notel,logs,MergeTree,42,1024\n"},
		{"datasources postgres query", "postgres", "select 1", metricFrame, "1700000000125"},
		{"datasources prometheus query", "prometheus", "up", metricFrame, "1700000000.125"},
		{"logs query", "loki", `{app="api"}`, logFrame, "original, line"},
		{"datasources loki query", "loki", `{app="api"}`, logFrame, "original, line"},
		{"logs metrics", "loki", `count_over_time({app="api"}[5m])`, metricFrame, "1700000000.125"},
		{"datasources loki metrics", "loki", `count_over_time({app="api"}[5m])`, metricFrame, "1700000000.125"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			testutils.SandboxConfigEnv(t)
			queries := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/query"):
					queries++
					_, err := w.Write([]byte(tt.response))
					assert.NoError(t, err)
				case req.URL.Path == "/api/datasources/uid/test-ds":
					_, err := fmt.Fprintf(w, `{"uid":"test-ds","type":%q}`, tt.kind)
					assert.NoError(t, err)
				default:
					http.NotFound(w, req)
				}
			}))
			defer server.Close()
			config := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(config, fmt.Appendf(nil, "version: 1\nstacks:\n  test:\n    grafana:\n      server: %s\n      token: test-token\n      org-id: 1\ncontexts:\n  test:\n    stack: test\ncurrent-context: test\n", server.URL), 0o600))
			cmd := root.Command("test")
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			args := strings.Fields(tt.path)
			if tt.expr != "" {
				args = append(args, tt.expr)
			}
			cmd.SetArgs(append(args, "-d", "test-ds", "--config", config, "-o", "csv"))
			require.NoError(t, cmd.Execute())
			assert.Equal(t, 1, queries)
			assert.Contains(t, stdout.String(), tt.want)
			records, err := csv.NewReader(&stdout).ReadAll()
			require.NoError(t, err)
			require.Len(t, records, 2)
		})
	}
}

func TestCSVIsExplicitlyRegistered(t *testing.T) {
	want := []string{
		"gcx datasources athena describe-table",
		"gcx datasources athena list-catalogs",
		"gcx datasources athena list-databases",
		"gcx datasources athena list-tables",
		"gcx datasources athena query",
		"gcx datasources azuremonitor logs query",
		"gcx datasources azuremonitor query",
		"gcx datasources azuremonitor resource-graph query",
		"gcx datasources bigquery describe-table",
		"gcx datasources bigquery list-datasets",
		"gcx datasources bigquery list-tables",
		"gcx datasources bigquery query",
		"gcx datasources clickhouse describe-table",
		"gcx datasources clickhouse list-tables",
		"gcx datasources clickhouse query",
		"gcx datasources cloudmonitoring query",
		"gcx datasources cloudwatch query",
		"gcx datasources elasticsearch metrics",
		"gcx datasources elasticsearch query",
		"gcx datasources infinity query",
		"gcx datasources influxdb query",
		"gcx datasources loki metrics",
		"gcx datasources loki query",
		"gcx datasources mssql describe-table",
		"gcx datasources mssql list-tables",
		"gcx datasources mssql query",
		"gcx datasources mysql describe-table",
		"gcx datasources mysql list-tables",
		"gcx datasources mysql query",
		"gcx datasources opensearch metrics",
		"gcx datasources opensearch query",
		"gcx datasources pinot query",
		"gcx datasources postgres describe-table",
		"gcx datasources postgres list-tables",
		"gcx datasources postgres query",
		"gcx datasources prometheus query",
		"gcx logs metrics",
		"gcx logs query",
		"gcx metrics billing query",
		"gcx metrics query",
	}
	var got []string
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if flag := cmd.Flags().Lookup("output"); flag != nil {
			_, formats, _ := strings.Cut(flag.Usage, "One of: ")
			if slices.Contains(strings.Split(formats, ", "), "csv") {
				got = append(got, cmd.CommandPath())
			}
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root.Command("test"))
	assert.ElementsMatch(t, want, got)
}

func TestLokiLimitWarningCommands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		format string
	}{
		{"logs csv", []string{"logs", "query", `{app="api"}`, "-d", "test-ds"}, "csv"},
		{"typed json", []string{"datasources", "loki", "query", `{app="api"}`, "-d", "test-ds"}, "json"},
		{"generic json", []string{"datasources", "query", "test-ds", `{app="api"}`}, "json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SandboxConfigEnv(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/query"):
					var body struct {
						Queries []struct {
							MaxLines int `json:"maxLines"`
						} `json:"queries"`
					}
					if !assert.NoError(t, json.NewDecoder(req.Body).Decode(&body)) || !assert.Len(t, body.Queries, 1) {
						return
					}
					assert.Equal(t, 1, body.Queries[0].MaxLines)
					fmt.Fprint(w, `{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"Time","type":"time"},{"name":"Line","type":"string"}]},"data":{"values":[[1700000000125],["original line"]]}}]}}}`)
				case req.URL.Path == "/api/datasources/uid/test-ds":
					fmt.Fprint(w, `{"uid":"test-ds","type":"loki"}`)
				default:
					http.NotFound(w, req)
				}
			}))
			defer server.Close()
			config := testutils.CreateTempFile(t, fmt.Sprintf("version: 1\nstacks:\n  test:\n    grafana:\n      server: %s\n      token: test-token\n      org-id: 1\ncontexts:\n  test:\n    stack: test\ncurrent-context: test\n", server.URL))
			cmd := root.Command("test")
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append(tc.args, "--config", config, "--limit", "1", "-o", tc.format))
			require.NoError(t, cmd.Execute())
			assert.Contains(t, stderr.String(), "query reached --limit 1; results may be incomplete")
			assert.NotContains(t, stdout.String(), "results may be incomplete")
			if tc.format == "csv" {
				records, err := csv.NewReader(&stdout).ReadAll()
				require.NoError(t, err)
				require.Len(t, records, 2)
				assert.Equal(t, "original line", records[1][1])
			} else {
				assert.True(t, json.Valid(stdout.Bytes()), stdout.String())
			}
		})
	}
}
