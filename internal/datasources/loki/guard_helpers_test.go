package loki_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newGuardTestServer fakes just enough of the Grafana/Loki HTTP surface for a
// full QueryCmd or MetricsCmd RunE to execute: the cloud-stack probe, the
// datasource-type lookup, the index-stats endpoint (counted separately, 6 GB
// per selector), and, for every other path, an empty log-stream query response
// (also counted, since a request reaching that branch means the real query ran).
func newGuardTestServer(t *testing.T, indexStatsRequests, otherRequests *int) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		// Checked before the datasource-lookup prefix below: the index-stats
		// path also starts with "/api/datasources/uid/".
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

func writeGuardTestConfig(t *testing.T, serverURL string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gcx-loki-guard-config-*.yaml")
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
