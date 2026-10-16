package root_test

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	for _, tt := range []struct{ path, kind, expr, response, want string }{
		{"metrics query", "prometheus", "up", metricFrame, "1700000000.125"},
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
			cmd.SetArgs(append(strings.Fields(tt.path), tt.expr, "-d", "test-ds", "--config", config, "-o", "csv"))
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
		"gcx datasources bigquery describe-table",
		"gcx datasources bigquery list-datasets",
		"gcx datasources bigquery list-tables",
		"gcx datasources bigquery query",
		"gcx datasources clickhouse describe-table",
		"gcx datasources clickhouse list-tables",
		"gcx datasources clickhouse query",
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
		if flag := cmd.Flags().Lookup("output"); flag != nil && strings.Contains(flag.Usage, "csv") {
			got = append(got, cmd.CommandPath())
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root.Command("test"))
	assert.ElementsMatch(t, want, got)
}
