package prometheus_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	dsprometheus "github.com/grafana/gcx/internal/datasources/prometheus"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryCmd_DrilldownLink(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case "/api/datasources/uid/prom-uid":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"uid":"prom-uid","type":"prometheus"}`))
			assert.NoError(t, err)
		case "/api/ds/query", "/apis/query.grafana.app/v0alpha1/namespaces/default/query":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
			assert.NoError(t, err)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	f, err := os.CreateTemp(t.TempDir(), "gcx-prom-config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(`
contexts:
  default:
    grafana:
      server: "` + srv.URL + `"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
current-context: default
`)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(f.Name())

	exec := func(args ...string) (string, string, error) {
		cmd := dsprometheus.QueryCmd(loader)
		root := &cobra.Command{Use: "test"}
		root.AddCommand(cmd)
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs(append([]string{"query", "-d", "prom-uid", "-o", "json"}, args...))
		err := root.Execute()
		return stdout.String(), stderr.String(), err
	}

	t.Run("prints drilldown link for a supported expression", func(t *testing.T) {
		_, stderr, err := exec("--drilldown-link", `rate(http_requests_total{job="grafana"}[5m])`)
		require.NoError(t, err)
		assert.Contains(t, stderr, "Metrics Drilldown link: ")
		assert.Contains(t, stderr, "/a/grafana-metricsdrilldown-app/drilldown")
	})

	t.Run("falls back to the explore link for an unsupported expression", func(t *testing.T) {
		_, stderr, err := exec("--drilldown-link", `sum(rate(a[5m]) + rate(b[5m]))`)
		require.NoError(t, err)
		assert.NotContains(t, stderr, "Metrics Drilldown link:")
		assert.Contains(t, stderr, "Explore link: ")
	})
}
