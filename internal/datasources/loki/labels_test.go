package loki_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	dsloki "github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeLabelsTestConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gcx-loki-config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// runLabelsCmd executes the labels command against a capture server with JSON
// output and returns the request query values recorded per resource path.
func runLabelsCmd(t *testing.T, args ...string) (map[string]url.Values, error) {
	t.Helper()
	_, captured, _, err := execLabelsCmd(t, append([]string{"-o", "json"}, args...)...)
	return captured, err
}

// runLabelsCmdRawOutput executes the labels command and returns raw stdout,
// leaving the output format to the caller's args.
func runLabelsCmdRawOutput(t *testing.T, args ...string) (string, error) {
	t.Helper()
	_, _, stdout, err := execLabelsCmd(t, args...)
	return stdout, err
}

// runLabelsCmdRequests executes the labels command and returns every request
// path the server saw, including config-loading calls like /bootdata, so
// callers can assert that validation ran before any network I/O at all.
func runLabelsCmdRequests(t *testing.T, args ...string) ([]string, error) {
	t.Helper()
	requests, _, _, err := execLabelsCmd(t, append([]string{"-o", "json"}, args...)...)
	return requests, err
}

// execLabelsCmd executes the labels command against a capture server and
// returns every request path seen (including config-loading calls such as
// /bootdata), the recorded query values per resource path, raw stdout, and
// the run error.
func execLabelsCmd(t *testing.T, args ...string) ([]string, map[string]url.Values, string, error) {
	t.Helper()

	var requests []string
	captured := map[string]url.Values{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
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

	cmd := dsloki.LabelsCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"labels", "-d", "loki-uid"}, args...))

	err := root.Execute()
	return requests, captured, stdout.String(), err
}

const (
	labelsPath      = "/api/datasources/uid/loki-uid/resources/labels"
	labelValuesPath = "/api/datasources/uid/loki-uid/resources/label/job/values"
)

func TestLabelsCmd_QueryScoping(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		path      string
		wantQuery string
	}{
		{
			name:      "no scoping sends no query param",
			args:      nil,
			path:      labelsPath,
			wantQuery: "",
		},
		{
			name:      "query is sent as the query param",
			args:      []string{"--query", `{app="backstage"}`},
			path:      labelsPath,
			wantQuery: `{app="backstage"}`,
		},
		{
			name:      "label values path gets the same query",
			args:      []string{"--label", "job", "--query", `{app="backstage"}`},
			path:      labelValuesPath,
			wantQuery: `{app="backstage"}`,
		},
		{
			name:      "label values path with no query sends no query param",
			args:      []string{"--label", "job"},
			path:      labelValuesPath,
			wantQuery: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			captured, err := runLabelsCmd(t, tc.args...)
			require.NoError(t, err)

			query, ok := captured[tc.path]
			require.True(t, ok, "expected a request to %s, got %v", tc.path, captured)
			assert.Equal(t, tc.wantQuery, query.Get("query"))
			assert.Len(t, captured, 1, "expected no other API requests")
		})
	}
}

// Guards against RunE bypassing the registered table codec (see #1050).
func TestLabelsCmd_TableOutputThroughCodec(t *testing.T) {
	stdout, err := runLabelsCmdRawOutput(t, "-o", "table")
	require.NoError(t, err)
	assert.Contains(t, stdout, "LABEL")
	assert.Contains(t, stdout, "job")
}

func TestLabelsCmd_RejectsPositionalArgs(t *testing.T) {
	requests, err := runLabelsCmdRequests(t, "job")
	require.Error(t, err)
	assert.Empty(t, requests, "no request of any kind, including config loading, should be made when args are rejected")
}

func TestLabelsCmd_RejectsExplicitlyEmptyFlagValues(t *testing.T) {
	for _, flag := range []string{"query", "label"} {
		t.Run(flag, func(t *testing.T) {
			requests, err := runLabelsCmdRequests(t, "--"+flag, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid --"+flag)
			assert.Empty(t, requests, "no request of any kind, including config loading, should be made for an empty --%s", flag)
		})
	}
}
