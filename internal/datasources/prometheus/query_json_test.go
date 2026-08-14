package prometheus_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	dsprometheus "github.com/grafana/gcx/internal/datasources/prometheus"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execQueryCmd runs `query -d prom-uid up` with the given extra arguments
// against a server that answers one instant sample, and returns stdout and
// the run error.
func execQueryCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case r.URL.Path == "/api/datasources/uid/prom-uid":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"id":1,"uid":"prom-uid","name":"prom","type":"prometheus"}`))
			assert.NoError(t, err)
		case r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[` +
				`{"name":"Time","type":"time"},` +
				`{"name":"Value","type":"number","labels":{"job":"grafana"}}]},` +
				`"data":{"values":[[1711893600000],[1]]}}]}}}`))
			assert.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	f, err := os.CreateTemp(t.TempDir(), "gcx-prom-query-*.yaml")
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

	// gcx silences both at the root, so a rejected selection writes nothing.
	root := &cobra.Command{Use: "test", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(dsprometheus.QueryCmd(loader))

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"query", "-d", "prom-uid", "up"}, args...))

	err = root.Execute()
	return stdout.String(), err
}

// TestQueryCmd_JSONPathInsideArrayIsRejected pins the array rule for
// `gcx metrics query --json data.result.metric`. The samples live in
// data.result, which is an array, and field selection walks maps only. The
// path once produced a silent null, so the message must name --jq instead.
func TestQueryCmd_JSONPathInsideArrayIsRejected(t *testing.T) {
	stdout, err := execQueryCmd(t, "--json", "data.result.metric")

	var arrayErr cmdio.ArrayPathSelectionError
	require.ErrorAs(t, err, &arrayErr)
	assert.Contains(t, err.Error(), "--json cannot reach a value inside an array: data.result.metric")
	assert.Contains(t, err.Error(), "--jq")
	assert.Empty(t, stdout, "a rejected selection must write nothing")
}

// TestQueryCmd_JSONPathOutsideArrayIsKept is the other half: a path that stops
// before the array still works.
func TestQueryCmd_JSONPathOutsideArrayIsKept(t *testing.T) {
	stdout, err := execQueryCmd(t, "--json", "data.resultType")

	require.NoError(t, err)
	assert.JSONEq(t, `{"data.resultType":"vector"}`, stdout)
}
