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

func TestListOptsSelectors(t *testing.T) {
	tests := []struct {
		name    string
		opts    listOpts
		want    []string
		wantErr string
	}{
		{
			name: "no filters and no match sends no selectors",
			want: nil,
		},
		{
			name: "prefix",
			opts: listOpts{Prefix: "http_"},
			want: []string{`{__name__=~"http_.*"}`},
		},
		{
			name: "suffix",
			opts: listOpts{Suffix: "_total"},
			want: []string{`{__name__=~".*_total"}`},
		},
		{
			name: "contains",
			opts: listOpts{Contains: "cart"},
			want: []string{`{__name__=~".*cart.*"}`},
		},
		{
			name: "filters combine with AND as stacked __name__ matchers",
			opts: listOpts{Prefix: "http_", Suffix: "_total", Contains: "requests"},
			want: []string{`{__name__=~"http_.*",__name__=~".*_total",__name__=~".*requests.*"}`},
		},
		{
			name: "regex metacharacters are escaped literally",
			opts: listOpts{Contains: "a.b"},
			want: []string{`{__name__=~".*a\\.b.*"}`},
		},
		{
			name: "filters fold into a match selector",
			opts: listOpts{Match: []string{`{job="api"}`}, Prefix: "http_"},
			want: []string{`{job="api",__name__=~"http_.*"}`},
		},
		{
			name: "filters fold into every match selector of the union",
			opts: listOpts{Match: []string{`{job="api"}`, `{job="worker"}`}, Suffix: "_total"},
			want: []string{`{job="api",__name__=~".*_total"}`, `{job="worker",__name__=~".*_total"}`},
		},
		{
			name: "match without filters passes through verbatim",
			opts: listOpts{Match: []string{`{job="api"}`, "up"}},
			want: []string{`{job="api"}`, "up"},
		},
		{
			name: "filters stack onto an existing __name__ regex matcher",
			opts: listOpts{Match: []string{`{__name__=~"http.*"}`}, Suffix: "_total"},
			want: []string{`{__name__=~"http.*",__name__=~".*_total"}`},
		},
		{
			name: "literal __name__ satisfying the filters is sent as written",
			opts: listOpts{Match: []string{`{__name__="http_requests_total",job="api"}`}, Prefix: "http_"},
			want: []string{`{__name__="http_requests_total",job="api"}`},
		},
		{
			name:    "literal __name__ contradicting the filters is rejected",
			opts:    listOpts{Match: []string{"up"}, Prefix: "http_"},
			wantErr: "contradict",
		},
		{
			name:    "invalid selector is rejected with the selector named",
			opts:    listOpts{Match: []string{`{job="api"`}, Prefix: "http_"},
			wantErr: "invalid --match selector",
		},
		{
			name:    "invalid selector is rejected without filters too",
			opts:    listOpts{Match: []string{`{job="api"`}},
			wantErr: "invalid --match selector",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.opts.selectors()
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNameFiltersAccept(t *testing.T) {
	assert.True(t, nameFiltersAccept("http_requests_total", "http_", "_total", "requests"))
	assert.False(t, nameFiltersAccept("http_requests_total", "http_", "_total", "CART"), "matching is case-sensitive")
	assert.False(t, nameFiltersAccept("up", "http_", "", ""))
	assert.True(t, nameFiltersAccept("anything", "", "", ""))
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
// raw keeps the undecoded bytes for serialization-shape assertions.
type listPayload struct {
	Data     []string        `json:"data"`
	ListMeta *cmdio.ListMeta `json:"list_meta"`
	raw      []byte
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
		resp = &listPayload{raw: stdout.Bytes()}
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

// TestListCmd_FiltersReachRequest drives each name filter through the command
// and asserts it arrives at the server as a __name__ regex in match[] — the
// filters are pushed down, not applied to the response (fast-follow on #1050).
func TestListCmd_FiltersReachRequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "prefix",
			args: []string{"--prefix", "http_"},
			want: []string{`{__name__=~"http_.*"}`},
		},
		{
			name: "suffix",
			args: []string{"--suffix", "_total"},
			want: []string{`{__name__=~".*_total"}`},
		},
		{
			name: "contains",
			args: []string{"--contains", "carts"},
			want: []string{`{__name__=~".*carts.*"}`},
		},
		{
			name: "combined filters AND within one selector",
			args: []string{"--prefix", "http_", "--suffix", "_total", "--contains", "requests"},
			want: []string{`{__name__=~"http_.*",__name__=~".*_total",__name__=~".*requests.*"}`},
		},
		{
			name: "filters fold into --match",
			args: []string{"--match", `{job="api"}`, "--prefix", "http_"},
			want: []string{`{job="api",__name__=~"http_.*"}`},
		},
	}

	names := []string{"http_requests_total"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			captured, resp, _, err := runListCmd(t, names, tc.args...)
			require.NoError(t, err)

			query, ok := captured[listNamesPath]
			require.True(t, ok, "expected a request to %s, got %v", listNamesPath, captured)
			assert.Equal(t, tc.want, query["match[]"])

			// The server owns filtering now: the response passes through.
			require.NotNil(t, resp)
			assert.Equal(t, names, resp.Data)
		})
	}
}

func TestListCmd_LimitReachesRequestOverFetchedByOne(t *testing.T) {
	captured, _, _, err := runListCmd(t, []string{"a_total", "b_total"}, "--limit", "2")
	require.NoError(t, err)
	require.Contains(t, captured, listNamesPath)
	assert.Equal(t, []string{"3"}, captured[listNamesPath]["limit"],
		"--limit N must reach the server as limit=N+1 so truncation stays detectable")

	captured, _, _, err = runListCmd(t, []string{"a_total"}, "--limit", "0")
	require.NoError(t, err)
	require.Contains(t, captured, listNamesPath)
	assert.NotContains(t, captured[listNamesPath], "limit", "--limit 0 must not send a limit param")
}

// TestListCmd_ServerTruncationYieldsPagedListMeta covers the backend that
// honors the limit param: exactly limit+1 names back means the over-fetch
// spare proved more data exists, and the total is unknown.
func TestListCmd_ServerTruncationYieldsPagedListMeta(t *testing.T) {
	_, resp, stderr, err := runListCmd(t, []string{"a_total", "b_total", "c_total"}, "--limit", "2")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, []string{"a_total", "b_total"}, resp.Data)

	require.NotNil(t, resp.ListMeta, "truncated page must carry list_meta")
	assert.True(t, resp.ListMeta.Truncated)
	assert.Equal(t, 2, resp.ListMeta.Returned)
	assert.Nil(t, resp.ListMeta.Total, "server-truncated source was not drained; total must stay unknown")
	assert.Contains(t, stderr, "showing first 2; more results are available")
}

// TestListCmd_LimitIgnoringBackendKeepsExactTotal covers the backend that
// predates the limit param: more than limit+1 names back means the response
// is the complete set, so the observed total is exact — the pre-pushdown UX.
func TestListCmd_LimitIgnoringBackendKeepsExactTotal(t *testing.T) {
	names := []string{"a_total", "b_total", "c_total", "d_total", "e_total"}

	_, resp, stderr, err := runListCmd(t, names, "--limit", "2")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, []string{"a_total", "b_total"}, resp.Data)

	require.NotNil(t, resp.ListMeta, "truncated page must carry list_meta")
	assert.True(t, resp.ListMeta.Truncated)
	assert.Equal(t, 2, resp.ListMeta.Returned)
	require.NotNil(t, resp.ListMeta.Total)
	assert.Equal(t, 5, *resp.ListMeta.Total)
	assert.Contains(t, stderr, "showing first 2 of 5")
}

func TestListCmd_CompleteResultOmitsListMeta(t *testing.T) {
	names := []string{"a_total", "b_total"}

	_, resp, stderr, err := runListCmd(t, names, "--limit", "2")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, names, resp.Data)
	assert.Nil(t, resp.ListMeta, "complete result set must omit list_meta")
	assert.NotContains(t, stderr, "showing first")

	_, resp, stderr, err = runListCmd(t, names, "--limit", "0")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, names, resp.Data)
	assert.Nil(t, resp.ListMeta, "complete result set must omit list_meta")
	assert.NotContains(t, stderr, "showing first")
}

// TestListCmd_ListMetaSurvivesFieldSelection guards the truncation signal on
// the --json path agent mode steers consumers toward: the payload must carry
// list_meta even under field selection (round 3 review on #1050).
func TestListCmd_ListMetaSurvivesFieldSelection(t *testing.T) {
	names := []string{"a_total", "b_total", "c_total", "d_total", "e_total"}

	_, resp, _, err := runListCmd(t, names, "--limit", "2", "--json", "data")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, []string{"a_total", "b_total"}, resp.Data)
	require.NotNil(t, resp.ListMeta, "list_meta must survive --json field selection")
	assert.True(t, resp.ListMeta.Truncated)
	assert.Equal(t, 2, resp.ListMeta.Returned)
}

func TestListCmd_RejectsPositionalArgs(t *testing.T) {
	captured, _, _, err := runListCmd(t, []string{"up"}, `{job="api"}`)
	require.Error(t, err)
	assert.Empty(t, captured, "no request should be made when args are rejected")
}

// TestListCmd_NullDataSerializesAsEmptyArray pins the zero-result shape: a
// backend answering data:null must still yield data:[] in the envelope.
func TestListCmd_NullDataSerializesAsEmptyArray(t *testing.T) {
	_, resp, _, err := runListCmd(t, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(resp.raw, &envelope))
	require.Contains(t, envelope, "data")
	assert.Equal(t, []any{}, envelope["data"], "data must serialize as [] rather than null")
}

func TestListCmd_RejectsExplicitlyEmptyFilters(t *testing.T) {
	for _, flag := range []string{"prefix", "suffix", "contains"} {
		t.Run(flag, func(t *testing.T) {
			captured, _, _, err := runListCmd(t, []string{"up"}, "--"+flag, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid --"+flag)
			assert.Empty(t, captured)
		})
	}
}

func TestListCmd_InvalidMatchSelectorFailsBeforeAnyRequest(t *testing.T) {
	captured, _, _, err := runListCmd(t, []string{"up"}, "--match", `{job="api"`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --match selector")
	assert.Empty(t, captured)
}

func TestListCmd_ContradictoryMatchAndFiltersFailBeforeAnyRequest(t *testing.T) {
	captured, _, _, err := runListCmd(t, []string{"up"}, "--match", "up", "--prefix", "http_")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contradict")
	assert.Empty(t, captured)
}

func TestListCmd_RejectsNegativeLimit(t *testing.T) {
	captured, _, _, err := runListCmd(t, []string{"up"}, "--limit", "-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit")
	assert.Empty(t, captured)
}
