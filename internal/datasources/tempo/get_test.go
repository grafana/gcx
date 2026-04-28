package tempo

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCmd_DoesNotLookupDatasourceTypeWithoutShareFlags(t *testing.T) {
	t.Helper()

	var traceCalls int
	var metadataCalls int
	var bootdataCalls int

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			bootdataCalls++
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case "/api/datasources/proxy/uid/tempo-uid/api/v2/traces/trace-123":
			traceCalls++
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"trace":{"traceID":"trace-123"}}`))
			require.NoError(t, err)
		case "/api/datasources/uid/tempo-uid":
			metadataCalls++
			http.Error(w, `{"message":"unexpected datasource lookup"}`, http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cfgFile := writeTempoTestConfig(t, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
    datasources:
      tempo: tempo-uid
current-context: default
`)

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	cmd := GetCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"get", "trace-123"})

	err := root.Execute()
	require.NoError(t, err)
	assert.Equal(t, 1, traceCalls)
	assert.Equal(t, 1, bootdataCalls)
	assert.Zero(t, metadataCalls)
	assert.Contains(t, stdout.String(), `"traceID": "trace-123"`)
	assert.Empty(t, stderr.String())
}

func writeTempoTestConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gcx-tempo-config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}
