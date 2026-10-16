package pyroscope_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

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

	t.Run("builds no link when --trace-id is set, with one explanation", func(t *testing.T) {
		for _, flag := range []string{"--drilldown-link", "--share-link"} {
			_, stderr, err := exec(`{service_name="frontend"}`, flag, "--trace-id", "4bf92f3577b34da6a3ce929d0e0e4736")
			require.NoError(t, err)
			assert.NotContains(t, stderr, "Profiles Drilldown link:", flag)
			assert.NotContains(t, stderr, "Explore link:", flag)
			assert.Equal(t, 1, strings.Count(stderr, "--trace-id has no representation"), flag)
		}
	})

	t.Run("default range matches the one-hour RPC window in both links", func(t *testing.T) {
		_, stderr, err := exec(`{service_name="frontend"}`, "--share-link", "--drilldown-link")
		require.NoError(t, err)

		explore := linkAfter(t, stderr, "Explore link: ")
		assert.Contains(t, explore, "now-1h")

		drilldown, err := url.Parse(linkAfter(t, stderr, "Profiles Drilldown link: "))
		require.NoError(t, err)
		from, err := strconv.ParseInt(drilldown.Query().Get("from"), 10, 64)
		require.NoError(t, err)
		to, err := strconv.ParseInt(drilldown.Query().Get("to"), 10, 64)
		require.NoError(t, err)
		assert.Equal(t, time.Hour, time.Duration(to-from)*time.Millisecond)
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

// linkAfter returns the URL printed after prefix on its stderr line.
func linkAfter(t *testing.T, stderr, prefix string) string {
	t.Helper()
	_, rest, found := strings.Cut(stderr, prefix)
	require.True(t, found, "missing %q in %q", prefix, stderr)
	line, _, _ := strings.Cut(rest, "\n")
	return strings.TrimSpace(line)
}
