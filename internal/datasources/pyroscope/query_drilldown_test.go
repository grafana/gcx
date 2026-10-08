package pyroscope_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	dspyroscope "github.com/grafana/gcx/internal/datasources/pyroscope"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const emptyFlamegraph = `{"flamegraph":{"names":[],"levels":[],"total":"0","maxSelf":"0"}}`

func TestQueryCmd_ExploreAndDrilldownLinks(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case "/api/datasources/uid/pyro-uid":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"uid":"pyro-uid","type":"grafana-pyroscope-datasource"}`))
			assert.NoError(t, err)
		case "/api/datasources/proxy/uid/pyro-uid/querier.v1.QuerierService/SelectMergeStacktraces":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(emptyFlamegraph))
			assert.NoError(t, err)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	f, err := os.CreateTemp(t.TempDir(), "gcx-pyro-config-*.yaml")
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

	exec := func(selector string, args ...string) (string, string, error) {
		cmd := dspyroscope.QueryCmd(loader)
		root := &cobra.Command{Use: "test"}
		root.AddCommand(cmd)
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs(append([]string{
			"query", "-d", "pyro-uid", "-o", "json",
			"--profile-type", "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
			selector,
		}, args...))
		err := root.Execute()
		return stdout.String(), stderr.String(), err
	}

	t.Run("prints explore link", func(t *testing.T) {
		_, stderr, err := exec(`{service_name="frontend"}`, "--share-link")
		require.NoError(t, err)
		assert.Contains(t, stderr, "Explore link: ")
	})

	t.Run("prints drilldown link for a supported selector", func(t *testing.T) {
		_, stderr, err := exec(`{service_name="frontend"}`, "--drilldown-link")
		require.NoError(t, err)
		assert.Contains(t, stderr, "Profiles Drilldown link: ")
		assert.Contains(t, stderr, "/a/grafana-pyroscope-app/explore")
	})

	t.Run("falls back to explore link when --trace-id forces it", func(t *testing.T) {
		_, stderr, err := exec(`{service_name="frontend"}`, "--drilldown-link", "--trace-id", "4bf92f3577b34da6a3ce929d0e0e4736")
		require.NoError(t, err)
		assert.NotContains(t, stderr, "Profiles Drilldown link:")
		assert.Contains(t, stderr, "Explore link: ")
	})

	t.Run("falls back to explore link when --stacktrace-selector forces it", func(t *testing.T) {
		_, stderr, err := exec(`{service_name="frontend"}`, "--drilldown-link", "--stacktrace-selector", "main.handler")
		require.NoError(t, err)
		assert.NotContains(t, stderr, "Profiles Drilldown link:")
		assert.Contains(t, stderr, "Explore link: ")
	})

	t.Run("falls back to explore link when selector has extra filters but no service_name", func(t *testing.T) {
		_, stderr, err := exec(`{env="prod"}`, "--drilldown-link")
		require.NoError(t, err)
		assert.NotContains(t, stderr, "Profiles Drilldown link:")
		assert.Contains(t, stderr, "Explore link: ")
	})
}
