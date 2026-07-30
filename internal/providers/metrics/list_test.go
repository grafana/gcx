//nolint:testpackage // Tests verify unexported command constructor wiring.
package metrics

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterMetricNames(t *testing.T) {
	names := []string{
		"http_requests_total",
		"http_request_duration_seconds_bucket",
		"app_cartservice_new_carts_created_total",
		"up",
	}

	tests := []struct {
		name     string
		prefix   string
		suffix   string
		contains string
		want     []string
	}{
		{
			name: "no filters returns all",
			want: names,
		},
		{
			name:   "prefix",
			prefix: "http_",
			want:   []string{"http_requests_total", "http_request_duration_seconds_bucket"},
		},
		{
			name:   "suffix",
			suffix: "_total",
			want:   []string{"http_requests_total", "app_cartservice_new_carts_created_total"},
		},
		{
			name:     "contains",
			contains: "cart",
			want:     []string{"app_cartservice_new_carts_created_total"},
		},
		{
			name:     "filters combine with AND",
			prefix:   "http_",
			suffix:   "_total",
			contains: "requests",
			want:     []string{"http_requests_total"},
		},
		{
			name:   "no match yields empty",
			prefix: "nomatch_",
			want:   []string{},
		},
		{
			name:     "case sensitive",
			contains: "CART",
			want:     []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterMetricNames(names, tc.prefix, tc.suffix, tc.contains)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestListCmd_Flags(t *testing.T) {
	cmd := listCmd(nil)
	require.Equal(t, "list-names", cmd.Name())

	for _, name := range []string{"datasource", "match", "prefix", "suffix", "contains", "limit", "output"} {
		assert.NotNil(t, cmd.Flags().Lookup(name), "missing flag --%s", name)
	}

	// Name filters are provider-specific options and must not have shorthands
	// (docs/design/naming.md 9.4).
	for _, name := range []string{"match", "prefix", "suffix", "contains", "limit"} {
		assert.Empty(t, cmd.Flags().Lookup(name).Shorthand, "--%s must not have a shorthand", name)
	}
}

const listNamesPath = "/api/datasources/uid/prom-uid/resources/api/v1/label/__name__/values"

// listPayload mirrors the metricNamesListResult envelope for decoding stdout.
type listPayload struct {
	Data     []string        `json:"data"`
	ListMeta *cmdio.ListMeta `json:"list_meta"`
}

// runListCmd executes the list command against a capture server returning the
// given metric names. It reports the captured query values per path along
// with the decoded stdout payload and stderr text.
func runListCmd(t *testing.T, names []string, args ...string) (map[string]url.Values, *listPayload, string, error) {
	t.Helper()

	captured := map[string]url.Values{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootdata" {
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			return
		}
		captured[r.URL.Path] = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(prometheus.LabelsResponse{Status: "success", Data: names}))
	}))
	defer srv.Close()

	f, err := os.CreateTemp(t.TempDir(), "gcx-metrics-config-*.yaml")
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

	cmd := listCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"list-names", "-d", "prom-uid", "-o", "json"}, args...))

	execErr := root.Execute()

	var resp *listPayload
	if execErr == nil && stdout.Len() > 0 {
		resp = &listPayload{}
		require.NoError(t, json.Unmarshal(stdout.Bytes(), resp))
	}
	return captured, resp, stderr.String(), execErr
}

func TestListCmd_MatchReachesRequest(t *testing.T) {
	captured, resp, _, err := runListCmd(t,
		[]string{"up", "http_requests_total"},
		"--match", `{job="api"}`, "--match", `{job="worker"}`,
	)
	require.NoError(t, err)

	query, ok := captured[listNamesPath]
	require.True(t, ok, "expected a request to %s, got %v", listNamesPath, captured)
	assert.Equal(t, []string{`{job="api"}`, `{job="worker"}`}, query["match[]"])
	require.NotNil(t, resp)
	assert.Equal(t, []string{"up", "http_requests_total"}, resp.Data)
}

func TestListCmd_LimitTruncatesWithListMetaAndHint(t *testing.T) {
	names := []string{"a_total", "b_total", "c_total", "d_total", "e_total"}

	captured, resp, stderr, err := runListCmd(t, names, "--limit", "2")
	require.NoError(t, err)
	require.Contains(t, captured, listNamesPath)
	require.NotNil(t, resp)
	assert.Equal(t, []string{"a_total", "b_total"}, resp.Data)

	// Truncation must be machine-legible in the payload itself, per the
	// list truncation contract (internal/output/listmeta.go).
	require.NotNil(t, resp.ListMeta, "truncated page must carry list_meta")
	assert.True(t, resp.ListMeta.Truncated)
	assert.Equal(t, 2, resp.ListMeta.Returned)
	require.NotNil(t, resp.ListMeta.Total)
	assert.Equal(t, 5, *resp.ListMeta.Total)
	assert.Contains(t, stderr, "showing first 2 of 5")

	_, resp, stderr, err = runListCmd(t, names, "--limit", "0")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Data, 5)
	assert.Nil(t, resp.ListMeta, "complete result set must omit list_meta")
	assert.NotContains(t, stderr, "showing first")
}

func TestListCmd_RejectsPositionalArgs(t *testing.T) {
	captured, _, _, err := runListCmd(t, []string{"up"}, `{job="api"}`)
	require.Error(t, err)
	assert.Empty(t, captured, "no request should be made when args are rejected")
}

func TestListCmd_RejectsNegativeLimit(t *testing.T) {
	captured, _, _, err := runListCmd(t, []string{"up"}, "--limit", "-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit")
	assert.Empty(t, captured)
}
