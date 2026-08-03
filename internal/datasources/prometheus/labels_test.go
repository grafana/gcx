package prometheus_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	dsprometheus "github.com/grafana/gcx/internal/datasources/prometheus"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeLabelsTestConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gcx-prom-config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// runLabelsCmd executes the labels command against a capture server with JSON
// output and returns the request query values recorded per path.
func runLabelsCmd(t *testing.T, args ...string) (map[string]url.Values, error) {
	t.Helper()
	captured, _, err := execLabelsCmd(t, append([]string{"-o", "json"}, args...)...)
	return captured, err
}

// runLabelsCmdRawOutput executes the labels command and returns raw stdout,
// leaving the output format to the caller's args.
func runLabelsCmdRawOutput(t *testing.T, args ...string) (string, error) {
	t.Helper()
	_, stdout, err := execLabelsCmd(t, args...)
	return stdout, err
}

// execLabelsCmd executes the labels command against a capture server and
// returns the recorded query values per path, raw stdout, and the run error.
func execLabelsCmd(t *testing.T, args ...string) (map[string]url.Values, string, error) {
	t.Helper()

	captured := map[string]url.Values{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootdata" {
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			return
		}
		captured[r.URL.Path] = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"status":"success","data":["job"]}`))
		assert.NoError(t, err)
	}))
	defer srv.Close()

	cfgFile := writeLabelsTestConfig(t, `
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

	cmd := dsprometheus.LabelsCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"labels", "-d", "prom-uid"}, args...))

	err := root.Execute()
	return captured, stdout.String(), err
}

const (
	labelsPath      = "/api/datasources/uid/prom-uid/resources/api/v1/labels"
	labelValuesPath = "/api/datasources/uid/prom-uid/resources/api/v1/label/job/values"
)

func TestLabelsCmd_MatchSelectors(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		path      string
		wantMatch []string
	}{
		{
			name:      "no scoping sends no match params",
			args:      nil,
			path:      labelsPath,
			wantMatch: nil,
		},
		{
			name:      "metric alone becomes a __name__ selector",
			args:      []string{"--metric", "http_requests_total"},
			path:      labelsPath,
			wantMatch: []string{`{__name__="http_requests_total"}`},
		},
		{
			name:      "dotted metric name is quoted, not sent bare",
			args:      []string{"--metric", "http.server.duration"},
			path:      labelsPath,
			wantMatch: []string{`{__name__="http.server.duration"}`},
		},
		{
			name:      "match selectors pass through verbatim without metric",
			args:      []string{"--match", `{cluster="prod"}`, "--match", `up{job="api"}`},
			path:      labelsPath,
			wantMatch: []string{`{cluster="prod"}`, `up{job="api"}`},
		},
		{
			name:      "metric folds into every match selector",
			args:      []string{"--metric", "http_requests_total", "--match", `{cluster="prod"}`, "--match", `{region=~"eu-.*"}`},
			path:      labelsPath,
			wantMatch: []string{`{cluster="prod",__name__="http_requests_total"}`, `{region=~"eu-.*",__name__="http_requests_total"}`},
		},
		{
			name:      "selector already naming the same metric is not double-folded",
			args:      []string{"--metric", "up", "--match", `up{job="api"}`},
			path:      labelsPath,
			wantMatch: []string{`{job="api",__name__="up"}`},
		},
		{
			name:      "consistent __name__ regex folds with the metric",
			args:      []string{"--metric", "http_requests_total", "--match", `{__name__=~"http_.*"}`},
			path:      labelsPath,
			wantMatch: []string{`{__name__=~"http_.*",__name__="http_requests_total"}`},
		},
		{
			name:      "quoted UTF-8 label names survive folding",
			args:      []string{"--metric", "up", "--match", `{"service.name"="cart"}`},
			path:      labelsPath,
			wantMatch: []string{`{"service.name"="cart",__name__="up"}`},
		},
		{
			name:      "label values path gets the same folded selectors",
			args:      []string{"--label", "job", "--metric", "http_requests_total", "--match", `{cluster="prod"}`},
			path:      labelValuesPath,
			wantMatch: []string{`{cluster="prod",__name__="http_requests_total"}`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			captured, err := runLabelsCmd(t, tc.args...)
			require.NoError(t, err)

			query, ok := captured[tc.path]
			require.True(t, ok, "expected a request to %s, got %v", tc.path, captured)
			assert.Equal(t, tc.wantMatch, query["match[]"])
			assert.Len(t, captured, 1, "expected no other API requests")
		})
	}
}

// TestLabelsCmd_TableOutputThroughCodec proves the registered shared codec
// renders the table path, guarding against a formatter bypass regressing
// (round 3 review on #1050: RunE previously short-circuited around it).
func TestLabelsCmd_TableOutputThroughCodec(t *testing.T) {
	stdout, err := runLabelsCmdRawOutput(t, "-o", "table")
	require.NoError(t, err)
	assert.Contains(t, stdout, "LABEL")
	assert.Contains(t, stdout, "job")
}

func TestLabelsCmd_RejectsExplicitlyEmptyFlagValues(t *testing.T) {
	for _, flag := range []string{"metric", "label"} {
		t.Run(flag, func(t *testing.T) {
			captured, err := runLabelsCmd(t, "--"+flag, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid --"+flag)
			assert.Empty(t, captured, "no request should be made for an empty --%s", flag)
		})
	}
}

func TestLabelsCmd_RejectsPositionalArgs(t *testing.T) {
	captured, err := runLabelsCmd(t, "http_requests_total")
	require.Error(t, err)
	assert.Empty(t, captured, "no request should be made when args are rejected")
}

func TestLabelsCmd_InvalidMatchSelectorFailsBeforeAnyRequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "with metric",
			args: []string{"--metric", "up", "--match", `{cluster="prod"`},
		},
		{
			name: "without metric",
			args: []string{"--match", `{cluster="prod"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			captured, err := runLabelsCmd(t, tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid --match selector")
			assert.Empty(t, captured)
		})
	}
}

func TestLabelsCmd_ContradictoryMetricFailsBeforeAnyRequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "selector names a different metric",
			args: []string{"--metric", "a", "--match", `b{x="y"}`},
		},
		{
			name: "__name__ regex excludes the metric",
			args: []string{"--metric", "up", "--match", `{__name__=~"http_.*"}`},
		},
		{
			name: "negative equality excludes the metric",
			args: []string{"--metric", "up", "--match", `{__name__!="up"}`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			captured, err := runLabelsCmd(t, tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "the intersection matches nothing")
			assert.Empty(t, captured)
		})
	}
}
