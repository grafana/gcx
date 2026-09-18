package loki_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	dsloki "github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeQueryTestConfig(t *testing.T, serverURL string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gcx-loki-config-*.yaml")
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

// TestQueryCmd_TUIRequiresInteractiveTerminal pins that --tui is rejected
// before any network I/O, not after. go test's stdout is never a TTY, so
// the guard must fire immediately; a regression that moves the check after
// datasource resolution or the actual Loki query would show up here as at
// least one recorded request.
func TestQueryCmd_TUIRequiresInteractiveTerminal(t *testing.T) {
	requests := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/bootdata" {
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
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
	root.SetArgs([]string{"query", "-d", "loki-uid", `{app="foo"}`, "--tui"})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires an interactive terminal")
	assert.Zero(t, requests, "no HTTP request should reach the backend when --tui is rejected before I/O")
}
