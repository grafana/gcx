package metrics_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	cmdfail "github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/metrics"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execCardinalityLabelNames runs `label-names -d prom-uid` with the given
// extra arguments against a server that answers one label name, and returns
// stdout and the run error.
func execCardinalityLabelNames(t *testing.T, args ...string) (string, error) {
	t.Helper()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case "/api/datasources/uid/prom-uid":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"id":1,"uid":"prom-uid","name":"prom","type":"prometheus"}`))
			assert.NoError(t, err)
		case "/api/datasources/uid/prom-uid/resources/api/v1/cardinality/label_names":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"label_values_count_total":12,"label_names_count":1,` +
				`"cardinality":[{"label_name":"job","label_values_count":12}]}`))
			assert.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	f, err := os.CreateTemp(t.TempDir(), "gcx-cardinality-*.yaml")
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
	root.AddCommand(metrics.CardinalityCommands(loader))

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"cardinality", "label-names", "-d", "prom-uid"}, args...))

	err = root.Execute()
	return stdout.String(), err
}

// TestCardinalityLabelNames_JSONPathInsideArrayIsRejected pins the array rule
// for `gcx metrics cardinality label-names --json cardinality.label_name`. The
// counts live in cardinality, which is an array, and field selection walks
// maps only. The path once produced a silent null, so the command must fail
// with the usage exit code and name --jq.
func TestCardinalityLabelNames_JSONPathInsideArrayIsRejected(t *testing.T) {
	stdout, err := execCardinalityLabelNames(t, "--json", "cardinality.label_name")

	var arrayErr cmdio.ArrayPathSelectionError
	require.ErrorAs(t, err, &arrayErr)
	assert.Contains(t, err.Error(), "--json cannot reach a value inside an array: cardinality.label_name")
	assert.Contains(t, err.Error(), "--jq")
	assert.Empty(t, stdout, "a rejected selection must write nothing")

	detailed := cmdfail.ErrorToDetailedError(err)
	require.NotNil(t, detailed)
	require.NotNil(t, detailed.ExitCode)
	assert.Equal(t, gcxerrors.ExitUsageError, *detailed.ExitCode)
}

// TestCardinalityLabelNames_JSONPathOutsideArrayIsKept is the other half: a
// path that stops before the array still works.
func TestCardinalityLabelNames_JSONPathOutsideArrayIsKept(t *testing.T) {
	stdout, err := execCardinalityLabelNames(t, "--json", "label_names_count")

	require.NoError(t, err)
	assert.JSONEq(t, `{"label_names_count":1}`, stdout)
}
