package loki_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	dsloki "github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newQueryPreflightTestServer fakes just enough of the Grafana/Loki HTTP
// surface for a full QueryCmd RunE to execute: the cloud-stack probe, the
// datasource-type lookup ResolveValidateAndSaveDatasource performs for a
// UID with no locally-known type, the index-stats endpoint (counted
// separately), and — for every other path — a generic empty log-stream
// query response (also counted, since a request reaching this branch is
// exactly what "the real query executed" means for these tests).
func newQueryPreflightTestServer(t *testing.T, indexStatsRequests, otherRequests *int) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		// Checked before the datasource-lookup prefix below, since the
		// index-stats path also starts with "/api/datasources/uid/" — the
		// more specific suffix must win, or every index-stats request gets
		// mistaken for a plain datasource-type lookup.
		case strings.HasSuffix(r.URL.Path, "/resources/index/stats"):
			*indexStatsRequests++
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"streams":1,"chunks":1,"bytes":6000000000,"entries":1}`))
			assert.NoError(t, err)
		case strings.HasPrefix(r.URL.Path, "/api/datasources/uid/"):
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"uid":"loki-uid","name":"loki-uid","type":"loki"}`))
			assert.NoError(t, err)
		default:
			*otherRequests++
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
			assert.NoError(t, err)
		}
	}))
}

func writeQueryTestConfig(t *testing.T, serverURL string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gcx-loki-query-config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(`
contexts:
  default:
    grafana:
      server: "` + serverURL + `"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
current-context: default
`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// execPreflightCmd runs newCmd against the fake server and returns stderr plus
// how many index-stats and other (real query) requests it made. Shared by
// QueryCmd and MetricsCmd, since each wires the pre-flight independently.
func execPreflightCmd(t *testing.T, newCmd func(*providers.ConfigLoader) *cobra.Command, use string, args ...string) (string, int, int) {
	t.Helper()

	var indexStatsRequests, otherRequests int
	srv := newQueryPreflightTestServer(t, &indexStatsRequests, &otherRequests)
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeQueryTestConfig(t, srv.URL))

	root := &cobra.Command{Use: "test"}
	root.AddCommand(newCmd(loader))

	var stdout, errBuf bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{use}, args...))

	require.NoError(t, root.Execute())
	return errBuf.String(), indexStatsRequests, otherRequests
}

func TestQueryCmd_ReportsStatsEstimateByDefaultAndStillRunsTheQuery(t *testing.T) {
	stderr, stats, other := execPreflightCmd(t, dsloki.QueryCmd, "query", "-d", "loki-uid", `{app="foo"}`)
	assert.Contains(t, stderr, "5.6 GiB")
	assert.Positive(t, stats)
	assert.Positive(t, other, "the real query must still run")
}

func TestMetricsCmd_ReportsStatsEstimateByDefaultAndStillRunsTheQuery(t *testing.T) {
	stderr, stats, other := execPreflightCmd(t, dsloki.MetricsCmd, "metrics", "-d", "loki-uid", `rate({app="foo"}[5m])`)
	assert.Contains(t, stderr, "5.6 GiB")
	assert.Positive(t, stats)
	assert.Positive(t, other, "the real query must still run")
}

func TestQueryCmd_WarnsAndStillRunsWhenOverThreshold(t *testing.T) {
	stderr, _, other := execPreflightCmd(t, dsloki.QueryCmd, "query", "-d", "loki-uid", `{app="foo"}`, "--stats-warn-bytes", "1GB")
	assert.Contains(t, stderr, "consider adding more filters or reducing the time range")
	assert.Positive(t, other, "an over-threshold estimate must not block the query")
}

func TestQueryCmd_SkipStatsMakesNoStatsRequest(t *testing.T) {
	stderr, stats, other := execPreflightCmd(t, dsloki.QueryCmd, "query", "-d", "loki-uid", `{app="foo"}`, "--skip-stats")
	assert.Zero(t, stats)
	assert.NotContains(t, stderr, "GiB")
	assert.Positive(t, other)
}
