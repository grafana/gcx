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

// newNativePathTestServer fakes just enough of the Grafana/Loki HTTP
// surface to run a full QueryCmd/MetricsCmd RunE, recording which of the two
// transports (Grafana's query-proxy vs Loki's own API via the resource
// proxy) the request actually used.
func newNativePathTestServer(t *testing.T, hitNative, hitProxy *bool) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case strings.Contains(r.URL.Path, "/resources/query"):
			*hitNative = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
		case strings.HasPrefix(r.URL.Path, "/api/datasources/uid/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"uid":"loki-uid","name":"loki-uid","type":"loki"}`))
		default:
			*hitProxy = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
		}
	}))
}

func writeNativePathTestConfig(t *testing.T, serverURL string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gcx-loki-nativepath-config-*.yaml")
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

func TestQueryCmd_NanosecondFromToUsesLokiNativeAPI(t *testing.T) {
	var hitNative, hitProxy bool
	srv := newNativePathTestServer(t, &hitNative, &hitProxy)
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeNativePathTestConfig(t, srv.URL))

	cmd := dsloki.QueryCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"query", "-d", "loki-uid", `{app="foo"}`,
		"--from", "1705315800123456789", "--to", "1705315801123456789",
	})

	require.NoError(t, root.Execute())
	assert.True(t, hitNative, "nanosecond-precision --from/--to should use Loki's native API")
	assert.False(t, hitProxy, "should not also hit Grafana's query-proxy")
}

func TestQueryCmd_MillisecondFromToUsesGrafanaQueryProxy(t *testing.T) {
	var hitNative, hitProxy bool
	srv := newNativePathTestServer(t, &hitNative, &hitProxy)
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeNativePathTestConfig(t, srv.URL))

	cmd := dsloki.QueryCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"query", "-d", "loki-uid", `{app="foo"}`,
		"--from", "1705315800", "--to", "1705315860",
	})

	require.NoError(t, root.Execute())
	assert.False(t, hitNative, "ordinary --from/--to should keep using Grafana's query-proxy, unchanged")
	assert.True(t, hitProxy)
}

func TestMetricsCmd_NanosecondFromToUsesLokiNativeAPI(t *testing.T) {
	var hitNative, hitProxy bool
	srv := newNativePathTestServer(t, &hitNative, &hitProxy)
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeNativePathTestConfig(t, srv.URL))

	cmd := dsloki.MetricsCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"metrics", "-d", "loki-uid", `rate({app="foo"}[5m])`,
		"--from", "1705315800123456789", "--to", "1705315801123456789",
	})

	require.NoError(t, root.Execute())
	assert.True(t, hitNative, "nanosecond-precision --from/--to should use Loki's native API")
	assert.False(t, hitProxy)
}
