package loki_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	dsloki "github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writePatternsTestConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gcx-loki-patterns-config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func execPatternsCmd(t *testing.T, args []string) (map[string]url.Values, string, error) {
	t.Helper()

	captured := map[string]url.Values{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootdata" {
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			return
		}
		captured[r.URL.Path] = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"status":"success","data":[{"pattern":"<_> level=error <_>","samples":[[1711839260,1],[1711839270,2]]}]}`))
		assert.NoError(t, err)
	}))
	defer srv.Close()

	cfgFile := writePatternsTestConfig(t, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
current-context: default
`)

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	cmd := dsloki.PatternsCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"patterns", "-d", "loki-uid"}, args...))

	err := root.Execute()
	return captured, stdout.String(), err
}

const patternsPath = "/api/datasources/uid/loki-uid/resources/patterns"

func TestPatternsCmd_TableOutput(t *testing.T) {
	captured, stdout, err := execPatternsCmd(t, []string{`{job="varlogs"}`, "--since", "1h"})
	require.NoError(t, err)

	query, ok := captured[patternsPath]
	require.True(t, ok, "expected a request to %s, got %v", patternsPath, captured)
	assert.Equal(t, `{job="varlogs"}`, query.Get("query"))
	assert.NotEmpty(t, query.Get("start"))
	assert.NotEmpty(t, query.Get("end"))

	assert.Contains(t, stdout, "PATTERN")
	assert.Contains(t, stdout, "SAMPLES")
	assert.Contains(t, stdout, "level=error")
	assert.Contains(t, stdout, "3") // 1 + 2
}

func TestPatternsCmd_JSONOutput(t *testing.T) {
	_, stdout, err := execPatternsCmd(t, []string{`{job="varlogs"}`, "--since", "1h", "-o", "json"})
	require.NoError(t, err)
	assert.Contains(t, stdout, `"pattern"`)
	assert.Contains(t, stdout, `"samples"`)
}

func TestPatternsCmd_DefaultsToOneHourWindowWhenNoTimeFlagsGiven(t *testing.T) {
	before := time.Now()
	captured, _, err := execPatternsCmd(t, []string{`{job="varlogs"}`})
	after := time.Now()
	require.NoError(t, err)

	query, ok := captured[patternsPath]
	require.True(t, ok, "expected a request to %s, got %v", patternsPath, captured)

	startNanos, err := strconv.ParseInt(query.Get("start"), 10, 64)
	require.NoError(t, err)
	endNanos, err := strconv.ParseInt(query.Get("end"), 10, 64)
	require.NoError(t, err)

	start := time.Unix(0, startNanos)
	end := time.Unix(0, endNanos)

	assert.WithinDuration(t, before, end, 5*time.Second, "end should default to now")
	assert.WithinDuration(t, before.Add(-time.Hour), start, 5*time.Second, "start should default to a 1h lookback")
	assert.False(t, end.After(after.Add(time.Second)), "end should not be in the future relative to test execution")
}

func TestPatternsCmd_PassesStepThrough(t *testing.T) {
	captured, _, err := execPatternsCmd(t, []string{`{job="varlogs"}`, "--since", "1h", "--step", "30s"})
	require.NoError(t, err)

	query, ok := captured[patternsPath]
	require.True(t, ok, "expected a request to %s, got %v", patternsPath, captured)
	assert.Equal(t, "30s", query.Get("step"))
}

func TestPatternsCmd_RequiresExpr(t *testing.T) {
	_, _, err := execPatternsCmd(t, nil)
	require.Error(t, err)
}
