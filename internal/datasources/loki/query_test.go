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

// assertStatsMaxBytesBlocks builds the command via newCmd, executes it with
// use/args plus --stats-max-bytes 1GB against a fake server whose
// index-stats endpoint always reports over that threshold, and asserts the
// real query never ran. Shared by QueryCmd and MetricsCmd, since the switch
// wiring the blocking check into RunE is independently duplicated per
// command — a mistake made only in one of them would otherwise go
// uncaught.
func assertStatsMaxBytesBlocks(t *testing.T, newCmd func(*providers.ConfigLoader) *cobra.Command, use string, args ...string) {
	t.Helper()

	indexStatsRequests := 0
	otherRequests := 0

	srv := newQueryPreflightTestServer(t, &indexStatsRequests, &otherRequests)
	defer srv.Close()

	cfgFile := writeQueryTestConfig(t, srv.URL)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	cmd := newCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{use}, append(args, "--stats-max-bytes", "1GB")...))

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeding --stats-max-bytes")
	assert.Positive(t, indexStatsRequests, "expected the index-stats pre-flight check to have run")
	assert.Zero(t, otherRequests, "the real query must never execute when --stats-max-bytes blocks it")
}

// TestQueryCmd_StatsMaxBytesBlocksTheRealQuery pins the core promise of
// --stats-max-bytes: when the index-stats estimate exceeds it, the real
// Loki query must never execute at all, not just print a warning after the
// fact.
func TestQueryCmd_StatsMaxBytesBlocksTheRealQuery(t *testing.T) {
	assertStatsMaxBytesBlocks(t, dsloki.QueryCmd, "query", "-d", "loki-uid", `{app="foo"}`)
}

// TestMetricsCmd_StatsMaxBytesBlocksTheRealQuery is MetricsCmd's counterpart.
func TestMetricsCmd_StatsMaxBytesBlocksTheRealQuery(t *testing.T) {
	assertStatsMaxBytesBlocks(t, dsloki.MetricsCmd, "metrics", "-d", "loki-uid", `rate({app="foo"}[5m])`)
}

// TestQueryCmd_SkipStatsBypassesStatsMaxBytes confirms --skip-stats is a
// full escape hatch: it disables the blocking check too, not just the
// warn-only one, so it stays a working override if --stats-max-bytes is
// ever too aggressive.
func TestQueryCmd_SkipStatsBypassesStatsMaxBytes(t *testing.T) {
	indexStatsRequests := 0
	otherRequests := 0

	srv := newQueryPreflightTestServer(t, &indexStatsRequests, &otherRequests)
	defer srv.Close()

	cfgFile := writeQueryTestConfig(t, srv.URL)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	cmd := dsloki.QueryCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"query", "-d", "loki-uid", `{app="foo"}`, "--stats-max-bytes", "1GB", "--skip-stats"})

	err := root.Execute()
	require.NoError(t, err)
	assert.Zero(t, indexStatsRequests, "expected the pre-flight check to be skipped entirely")
	assert.Positive(t, otherRequests, "expected the real query to run when --skip-stats bypasses the block")
}
