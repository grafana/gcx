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
	for _, tt := range []struct {
		path, kind, expr, response, want string
		endpoint, wantError              string
		flags                            []string
	}{
		{path: "metrics query", kind: "prometheus", expr: "up", response: metricFrame, want: "1700000000.125"},
		{path: "datasources query test-ds", kind: "prometheus", expr: "up", response: metricFrame, want: "1700000000.125"},
		{path: "datasources clickhouse list-tables", kind: "clickhouse", expr: "", response: tableFrame, want: "database,name,engine,total_rows,total_bytes\notel,logs,MergeTree,42,1024\n"},
		{path: "datasources postgres query", kind: "postgres", expr: "select 1", response: metricFrame, want: "1700000000125"},
		{path: "datasources prometheus query", kind: "prometheus", expr: "up", response: metricFrame, want: "1700000000.125"},
		{path: "logs query", kind: "loki", expr: `{app="api"}`, response: logFrame, want: "original, line"},
		{path: "datasources loki query", kind: "loki", expr: `{app="api"}`, response: logFrame, want: "original, line"},
		{path: "logs query", kind: "loki", expr: `{app="api"}`, response: `{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"Time","type":"time"},{"name":"Body","type":"string"},{"name":"Value","type":"number"}]},"data":{"values":[[1700000000125],["legacy line"],[3.5]]}}]}}}`, want: "legacy line"},
		{path: "logs metrics", kind: "loki", expr: `count_over_time({app="api"}[5m])`, response: metricFrame, want: "1700000000.125"},
		{path: "datasources loki metrics", kind: "loki", expr: `count_over_time({app="api"}[5m])`, response: metricFrame, want: "1700000000.125"},
		{path: "datasources query test-ds", kind: "loki", expr: `{app="api"}`, response: logFrame, want: "original, line"},
		{path: "datasources query test-ds", kind: "loki", expr: `rate({app="api"}[5m])`, response: metricFrame, flags: []string{"--since", "1h"}, wantError: "use `gcx logs metrics`"},
		{path: "logs query", kind: "loki", expr: `rate({app="api"}[5m])`, response: metricFrame, wantError: "use `gcx logs metrics`"},
		{path: "datasources tempo metrics", kind: "tempo", expr: "{} | rate()", endpoint: "/api/datasources/proxy/uid/test-ds/api/metrics/query", response: `{"series":[{"labels":[{"key":"service","value":{"stringValue":"api"}}],"value":3.25}]}`, want: "value,label.service\n3.25,api\n"},
		{path: "datasources tempo metrics", kind: "tempo", expr: "{} | rate()", flags: []string{"--since", "1h"}, endpoint: "/api/datasources/proxy/uid/test-ds/api/metrics/query_range", response: `{"series":[{"samples":[{"timestampMs":"1700000000123","value":3.25}]}]}`, want: "timestamp_unix_ms,value\n1700000000123,3.25\n"},
		{path: "datasources tempo query", kind: "tempo", expr: "{}", endpoint: "/api/datasources/proxy/uid/test-ds/api/search", response: `{"traces":[{"traceID":"id","rootServiceName":"api","rootTraceName":"GET /","startTimeUnixNano":"1700000000123456789","durationMs":123}]}`, want: "id,api,GET /,1700000000123456789,123,"},
		{path: "profiles query", kind: "pyroscope", expr: "{}", flags: []string{"--profile-type", "process_cpu:cpu:nanoseconds:cpu:nanoseconds"}, endpoint: "/api/datasources/proxy/uid/test-ds/querier.v1.QuerierService/SelectMergeStacktraces", response: `{"flamegraph":{"names":["fn","other"],"levels":[{"values":["0","8","6","0","0","2","2","1"]}],"total":"10"}}`, want: "function,self,total,self_percentage\nfn,6,8,60\n"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			testutils.SandboxConfigEnv(t)
			queries := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case tt.endpoint != "" && req.URL.Path == tt.endpoint, tt.endpoint == "" && req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/query"):
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
			args = append(args, "--config", config, "-o", "csv")
			if tt.path != "datasources query test-ds" {
				args = append(args, "-d", "test-ds")
			}
			args = append(args, tt.flags...)
			cmd.SetArgs(args)
			err := cmd.Execute()
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				assert.Empty(t, stdout.String())
				assert.Equal(t, 1, queries)
				return
			}
			require.NoError(t, err)
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
		"gcx datasources pyroscope query",
		"gcx datasources query",
		"gcx datasources tempo baseline",
		"gcx datasources tempo metrics",
		"gcx datasources tempo query",
		"gcx logs metrics",
		"gcx logs query",
		"gcx metrics billing query",
		"gcx metrics query",
		"gcx profiles query",
		"gcx traces baseline",
		"gcx traces metrics",
		"gcx traces query",
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
