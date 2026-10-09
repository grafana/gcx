package loki_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync/atomic"
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

func execQueryPatternsCmd(t *testing.T, args []string) (map[string]url.Values, string, error) {
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

	cmd := dsloki.QueryPatternsCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"query-patterns", "-d", "loki-uid"}, args...))

	err := root.Execute()
	return captured, stdout.String(), err
}

const patternsPath = "/api/datasources/uid/loki-uid/resources/patterns"

func TestQueryPatternsCmd_TableOutput(t *testing.T) {
	captured, stdout, err := execQueryPatternsCmd(t, []string{`{job="varlogs"}`, "--since", "1h"})
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

func TestQueryPatternsCmd_JSONOutput(t *testing.T) {
	_, stdout, err := execQueryPatternsCmd(t, []string{`{job="varlogs"}`, "--since", "1h", "-o", "json"})
	require.NoError(t, err)
	assert.Contains(t, stdout, `"pattern"`)
	assert.Contains(t, stdout, `"samples"`)
}

func TestQueryPatternsCmd_DefaultsToOneHourWindowWhenNoTimeFlagsGiven(t *testing.T) {
	before := time.Now()
	captured, _, err := execQueryPatternsCmd(t, []string{`{job="varlogs"}`})
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

func TestQueryPatternsCmd_PassesStepThrough(t *testing.T) {
	captured, _, err := execQueryPatternsCmd(t, []string{`{job="varlogs"}`, "--since", "1h", "--step", "30s"})
	require.NoError(t, err)

	query, ok := captured[patternsPath]
	require.True(t, ok, "expected a request to %s, got %v", patternsPath, captured)
	assert.Equal(t, "30s", query.Get("step"))
}

func TestQueryPatternsCmd_RequiresExpr(t *testing.T) {
	_, _, err := execQueryPatternsCmd(t, nil)
	require.Error(t, err)
}

// Loki's patterns endpoint accepts step as either a duration string ("30s")
// or a bare float number of seconds ("30", "1.5") — using shared.ParseTimes
// (which validates --step through Go-duration-only syntax) instead of
// shared.ParseTimeRange would reject the latter before any request is sent.
func TestQueryPatternsCmd_AcceptsBareSecondsStep(t *testing.T) {
	captured, _, err := execQueryPatternsCmd(t, []string{`{job="varlogs"}`, "--since", "1h", "--step", "30"})
	require.NoError(t, err)

	query, ok := captured[patternsPath]
	require.True(t, ok, "expected a request to %s, got %v", patternsPath, captured)
	assert.Equal(t, "30", query.Get("step"))
}

func TestQueryPatternsCmd_WideOutputMatchesTable(t *testing.T) {
	_, table, err := execQueryPatternsCmd(t, []string{`{job="varlogs"}`, "--since", "1h", "-o", "table"})
	require.NoError(t, err)

	_, wide, err := execQueryPatternsCmd(t, []string{`{job="varlogs"}`, "--since", "1h", "-o", "wide"})
	require.NoError(t, err)
	assert.Equal(t, table, wide)
}

// requestsMadeBy runs query-patterns against a server that counts every request
// (including the cloud-stack probe), to prove invalid input fails before any I/O.
func requestsMadeBy(t *testing.T, args []string) (int, error) {
	t.Helper()

	var requests atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, `{"message":"unexpected request"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writePatternsTestConfig(t, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
current-context: default
`))

	root := &cobra.Command{Use: "test"}
	root.AddCommand(dsloki.QueryPatternsCmd(loader))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"query-patterns", "-d", "loki-uid"}, args...))

	err := root.Execute()
	return int(requests.Load()), err
}

func TestQueryPatternsCmd_RejectsStaticallyInvalidInputBeforeAnyIO(t *testing.T) {
	tests := map[string]struct {
		args    []string
		wantErr string
	}{
		"empty selector":            {[]string{""}, "stream selector is required"},
		"whitespace selector":       {[]string{"   "}, "stream selector is required"},
		"selector with line filter": {[]string{`{job="x"} |= "err"`}, "bare stream selector"},
		"metric expression":         {[]string{`rate({job="x"}[5m])`}, "bare stream selector"},
		"step zero":                 {[]string{`{job="x"}`, "--step", "0"}, "invalid --step"},
		"step negative":             {[]string{`{job="x"}`, "--step", "-1"}, "invalid --step"},
		"step negative duration":    {[]string{`{job="x"}`, "--step", "-5s"}, "invalid --step"},
		"step zero duration":        {[]string{`{job="x"}`, "--step", "0s"}, "invalid --step"},
		"step garbage":              {[]string{`{job="x"}`, "--step", "bogus"}, "invalid --step"},
		"step infinite":             {[]string{`{job="x"}`, "--step", "Inf"}, "invalid --step"},
		"reversed range":            {[]string{`{job="x"}`, "--from", "now", "--to", "now-1h"}, "must be before"},
		"malformed time":            {[]string{`{job="x"}`, "--from", "not-a-time", "--to", "now"}, "invalid --from"},
		"from without to":           {[]string{`{job="x"}`, "--from", "now-1h"}, "--to is required"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			requests, err := requestsMadeBy(t, tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Zero(t, requests, "no request, including config/datasource discovery, may precede validation")
		})
	}
}

func TestQueryPatternsCmd_AcceptsDocumentedStepForms(t *testing.T) {
	for _, step := range []string{"30s", "1m", "30", "1.5"} {
		captured, _, err := execQueryPatternsCmd(t, []string{`{job="varlogs"}`, "--since", "1h", "--step", step})
		require.NoError(t, err, step)
		assert.Equal(t, step, captured[patternsPath].Get("step"), step)
	}
}

func TestQueryPatternsCmd_AcceptsSelectorWithWhitespaceAndMultipleMatchers(t *testing.T) {
	_, _, err := execQueryPatternsCmd(t, []string{`  { job="varlogs", namespace=~"a|b" }  `, "--since", "1h"})
	require.NoError(t, err)
}
