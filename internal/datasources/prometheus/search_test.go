package prometheus_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	dsprometheus "github.com/grafana/gcx/internal/datasources/prometheus"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSearchTestConfig(t *testing.T, serverURL string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gcx-prom-search-config-*.yaml")
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

// newSearchTestRoot builds a root command with all three search-* leaf
// commands mounted, against a capture server that answers every request
// with the given NDJSON body.
func newSearchTestRoot(t *testing.T, ndjson string) (*cobra.Command, *bytes.Buffer, func() (string, url.Values)) {
	t.Helper()

	var (
		capturedPath  string
		capturedQuery url.Values
	)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootdata" {
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			return
		}
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		_, _ = w.Write([]byte(ndjson))
	}))
	t.Cleanup(srv.Close)

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeSearchTestConfig(t, srv.URL))

	root := &cobra.Command{Use: "test"}
	root.AddCommand(
		dsprometheus.SearchMetricNamesCmd(loader),
		dsprometheus.SearchLabelNamesCmd(loader),
		dsprometheus.SearchLabelValuesCmd(loader),
	)

	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&bytes.Buffer{})

	return root, &stdout, func() (string, url.Values) { return capturedPath, capturedQuery }
}

func TestSearchMetricNamesCmd(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"name":"up","score":95}]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	root, stdout, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-metric-names", "up", "-d", "prom-uid", "-o", "json", "--include-score"})
	require.NoError(t, root.Execute())

	path, query := captured()
	assert.Equal(t, "/api/datasources/uid/prom-uid/resources/api/v1/search/metric_names", path)
	assert.Equal(t, []string{"up"}, query["search[]"])
	assert.Equal(t, "true", query.Get("include_score"))
	assert.Contains(t, stdout.String(), `"name": "up"`)
	assert.Contains(t, stdout.String(), `"score": 95`)
}

func TestSearchLabelNamesCmd(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"name":"job"}]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	root, stdout, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-label-names", "jo", "-d", "prom-uid", "-o", "json"})
	require.NoError(t, root.Execute())

	path, query := captured()
	assert.Equal(t, "/api/datasources/uid/prom-uid/resources/api/v1/search/label_names", path)
	assert.Equal(t, []string{"jo"}, query["search[]"])
	assert.Contains(t, stdout.String(), `"name": "job"`)
}

// TestSearchCmds_CaseSensitiveDefaultsFalse proves case_sensitive=false is
// sent by default on every search command, and that --case-sensitive still
// overrides it.
func TestSearchCmds_CaseSensitiveDefaultsFalse(t *testing.T) {
	ndjson := strings.Join([]string{`{"results":[]}`, `{"status":"success"}`}, "\n") + "\n"

	tests := []struct {
		name string
		args []string
	}{
		{name: "search-metric-names", args: []string{"search-metric-names", "up", "-d", "prom-uid"}},
		{name: "search-label-names", args: []string{"search-label-names", "jo", "-d", "prom-uid"}},
		{name: "search-label-values", args: []string{"search-label-values", "job", "pro", "-d", "prom-uid"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, _, captured := newSearchTestRoot(t, ndjson)
			root.SetArgs(tc.args)
			require.NoError(t, root.Execute())

			_, query := captured()
			assert.Equal(t, "false", query.Get("case_sensitive"))
		})
	}
}

func TestSearchLabelNamesCmd_CaseSensitiveFlagOverridesDefault(t *testing.T) {
	ndjson := strings.Join([]string{`{"results":[]}`, `{"status":"success"}`}, "\n") + "\n"

	root, _, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-label-names", "jo", "-d", "prom-uid", "--case-sensitive"})
	require.NoError(t, root.Execute())

	_, query := captured()
	assert.Equal(t, "true", query.Get("case_sensitive"))
}

func TestSearchLabelValuesCmd(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"value":"prometheus"}]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	root, stdout, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-label-values", "job", "prom", "-d", "prom-uid", "-o", "json"})
	require.NoError(t, root.Execute())

	path, query := captured()
	assert.Equal(t, "/api/datasources/uid/prom-uid/resources/api/v1/search/label_values", path)
	assert.Equal(t, "job", query.Get("label"))
	assert.Equal(t, []string{"prom"}, query["search[]"])
	assert.Contains(t, stdout.String(), `"value": "prometheus"`)
}

func TestSearchMetricNamesCmd_RequiresTerm(t *testing.T) {
	root, _, captured := newSearchTestRoot(t, "")
	root.SetArgs([]string{"search-metric-names", "-d", "prom-uid", "-o", "json"})

	err := root.Execute()
	require.Error(t, err)

	path, _ := captured()
	assert.Empty(t, path, "no request should be made when no search term is given")
}

func TestSearchLabelNamesCmd_RequiresTerm(t *testing.T) {
	root, _, captured := newSearchTestRoot(t, "")
	root.SetArgs([]string{"search-label-names", "-d", "prom-uid", "-o", "json"})

	err := root.Execute()
	require.Error(t, err)

	path, _ := captured()
	assert.Empty(t, path, "no request should be made when no search term is given")
}

func TestSearchLabelValuesCmd_RequiresLabelArg(t *testing.T) {
	root, _, captured := newSearchTestRoot(t, "")
	root.SetArgs([]string{"search-label-values", "-d", "prom-uid", "-o", "json"})

	err := root.Execute()
	require.Error(t, err)

	path, _ := captured()
	assert.Empty(t, path, "no request should be made when the required LABEL arg is missing")
}

// TestSearchLabelValuesCmd_RejectsExplicitlyEmptyLabel proves an explicitly
// empty LABEL positional (typically an unset shell variable, as in
// `search-label-values "$LABEL"`) is rejected before any request, the same
// way rejectExplicitlyEmptyFlags guards --metric/--metric-regex.
func TestSearchLabelValuesCmd_RejectsExplicitlyEmptyLabel(t *testing.T) {
	root, _, captured := newSearchTestRoot(t, "")
	root.SetArgs([]string{"search-label-values", "", "pro", "-d", "prom-uid", "-o", "json"})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid LABEL")

	path, _ := captured()
	assert.Empty(t, path, "no request should be made for an empty LABEL")
}

func TestSearchMetricNamesCmd_TableOutput(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"name":"up","score":95,"type":"gauge","help":"1 if up"}]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	root, stdout, _ := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-metric-names", "up", "-d", "prom-uid", "-o", "table", "--include-score", "--include-metadata"})
	require.NoError(t, root.Execute())

	out := stdout.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "SCORE")
	assert.Contains(t, out, "TYPE")
	assert.Contains(t, out, "up")
	assert.Contains(t, out, "gauge")
}

// TestSearchMetricNamesCmd_TableOutput_FractionalScore proves a fractional
// score renders cleanly (via strconv.FormatFloat), not truncated to an
// integer and not over-precise.
func TestSearchMetricNamesCmd_TableOutput_FractionalScore(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"name":"up","score":92.5}]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	root, stdout, _ := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-metric-names", "up", "-d", "prom-uid", "-o", "table", "--include-score"})
	require.NoError(t, root.Execute())

	out := stdout.String()
	assert.Contains(t, out, "92.5")
	assert.NotContains(t, out, "92.500000")
}

func TestSearchLabelNamesCmd_MetricFoldsIntoMatch(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"name":"job"}]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	root, _, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-label-names", "jo", "-d", "prom-uid", "--metric", "http_requests_total"})
	require.NoError(t, root.Execute())

	_, query := captured()
	assert.Equal(t, []string{`{__name__="http_requests_total"}`}, query["match[]"])
}

func TestSearchLabelValuesCmd_MetricFoldsIntoMatch(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"value":"prometheus"}]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	root, _, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-label-values", "job", "prom", "-d", "prom-uid", "--metric", "http_requests_total"})
	require.NoError(t, root.Execute())

	_, query := captured()
	assert.Equal(t, []string{`{__name__="http_requests_total"}`}, query["match[]"])
}

func TestSearchLabelNamesCmd_MetricCombinesWithMatch(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	root, _, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{
		"search-label-names", "jo", "-d", "prom-uid",
		"--metric", "http_requests_total",
		"--match", `{cluster="prod"}`,
	})
	require.NoError(t, root.Execute())

	_, query := captured()
	assert.Equal(t, []string{`{cluster="prod",__name__="http_requests_total"}`}, query["match[]"])
}

func TestSearchLabelNamesCmd_ContradictoryMetricFailsBeforeAnyRequest(t *testing.T) {
	root, _, captured := newSearchTestRoot(t, "")
	root.SetArgs([]string{
		"search-label-names", "jo", "-d", "prom-uid",
		"--metric", "up",
		"--match", `{__name__="down"}`,
	})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the intersection matches nothing")

	path, _ := captured()
	assert.Empty(t, path, "no request should be made when --metric contradicts --match")
}

func TestSearchLabelNamesCmd_RejectsExplicitlyEmptyMetric(t *testing.T) {
	root, _, captured := newSearchTestRoot(t, "")
	root.SetArgs([]string{"search-label-names", "jo", "-d", "prom-uid", "--metric", ""})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --metric")

	path, _ := captured()
	assert.Empty(t, path, "no request should be made for an empty --metric")
}

// TestSearchMetricNamesCmd_RejectsInvalidEnumAndRangeFlags proves --sort-by,
// --sort-dir, --fuzz-alg, --fuzz-threshold, and --limit are validated
// client-side against their documented allowed values/ranges before any
// request is made — an unvalidated bad value would otherwise reach the
// server as an opaque error.
func TestSearchMetricNamesCmd_RejectsInvalidEnumAndRangeFlags(t *testing.T) {
	tests := []struct {
		name      string
		extraArgs []string
		wantErr   string
	}{
		{name: "bad sort-by", extraArgs: []string{"--sort-by", "bogus"}, wantErr: "invalid --sort-by"},
		{name: "explicitly empty sort-by", extraArgs: []string{"--sort-by", ""}, wantErr: "invalid --sort-by"},
		{name: "bad sort-dir", extraArgs: []string{"--sort-dir", "bogus"}, wantErr: "invalid --sort-dir"},
		{name: "sort-dir incompatible with sort-by=score", extraArgs: []string{"--sort-dir", "asc"}, wantErr: "incompatible with --sort-by=score"},
		{name: "bad fuzz-alg", extraArgs: []string{"--fuzz-alg", "bogus"}, wantErr: "invalid --fuzz-alg"},
		{name: "fuzz-threshold too low", extraArgs: []string{"--fuzz-threshold", "-1"}, wantErr: "invalid --fuzz-threshold"},
		{name: "fuzz-threshold too high", extraArgs: []string{"--fuzz-threshold", "101"}, wantErr: "invalid --fuzz-threshold"},
		{name: "negative limit", extraArgs: []string{"--limit", "-1"}, wantErr: "invalid --limit"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, _, captured := newSearchTestRoot(t, "")
			args := append([]string{"search-metric-names", "up", "-d", "prom-uid"}, tc.extraArgs...)
			root.SetArgs(args)

			err := root.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)

			path, _ := captured()
			assert.Empty(t, path, "no request should be made for an invalid flag value")
		})
	}
}

// TestSearchMetricNamesCmd_AcceptsValidEnumAndRangeFlags proves the
// documented valid values for the same flags are not rejected.
func TestSearchMetricNamesCmd_AcceptsValidEnumAndRangeFlags(t *testing.T) {
	ndjson := strings.Join([]string{`{"results":[]}`, `{"status":"success"}`}, "\n") + "\n"

	tests := [][]string{
		{"--sort-by", "alpha"},
		{"--sort-by", "alpha", "--sort-dir", "dsc"},
		{"--fuzz-alg", "subsequence"},
		{"--fuzz-threshold", "0"},
		{"--fuzz-threshold", "100"},
		{"--limit", "0"},
	}

	for _, extraArgs := range tests {
		t.Run(strings.Join(extraArgs, " "), func(t *testing.T) {
			root, _, _ := newSearchTestRoot(t, ndjson)
			args := append([]string{"search-metric-names", "up", "-d", "prom-uid"}, extraArgs...)
			root.SetArgs(args)
			require.NoError(t, root.Execute())
		})
	}
}

func TestSearchMetricNamesCmd_HasNoMetricFlag(t *testing.T) {
	root, _, _ := newSearchTestRoot(t, "")
	cmd, _, err := root.Find([]string{"search-metric-names"})
	require.NoError(t, err)
	assert.Nil(t, cmd.Flags().Lookup("metric"), "search-metric-names should not have --metric: restricting to one exact metric name defeats a fuzzy metric search")
	assert.Nil(t, cmd.Flags().Lookup("metric-regex"), "search-metric-names should not have --metric-regex either")
}

func TestSearchLabelNamesCmd_MetricRegexFoldsIntoMatchRawNoEscaping(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	root, _, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-label-names", "jo", "-d", "prom-uid", "--metric-regex", `^http_.*\.total$`})
	require.NoError(t, root.Execute())

	_, query := captured()
	// The pattern is used exactly as given: not escaped, not wrapped in a
	// "contains" template. A literal "." stays a regex "any character",
	// proving no regexp.QuoteMeta was applied.
	assert.Equal(t, []string{`{__name__=~"^http_.*\\.total$"}`}, query["match[]"])
}

func TestSearchLabelNamesCmd_MetricRegexRejectsInvalidPattern(t *testing.T) {
	root, _, captured := newSearchTestRoot(t, "")
	root.SetArgs([]string{"search-label-names", "jo", "-d", "prom-uid", "--metric-regex", "("})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --metric-regex")

	path, _ := captured()
	assert.Empty(t, path, "no request should be made for an invalid --metric-regex pattern")
}

func TestSearchLabelNamesCmd_MetricAndRegexMutuallyExclusive(t *testing.T) {
	root, _, captured := newSearchTestRoot(t, "")
	root.SetArgs([]string{"search-label-names", "jo", "-d", "prom-uid", "--metric", "up", "--metric-regex", "up.*"})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")

	path, _ := captured()
	assert.Empty(t, path, "no request should be made when --metric and --metric-regex are both set")
}

func TestSearchLabelNamesCmd_RequiresTermUnlessMetricScopeGiven(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "no term, no scope: error", args: []string{"search-label-names", "-d", "prom-uid"}, wantErr: true},
		{name: "no term, --metric: ok", args: []string{"search-label-names", "-d", "prom-uid", "--metric", "up"}, wantErr: false},
		{name: "no term, --metric-regex: ok", args: []string{"search-label-names", "-d", "prom-uid", "--metric-regex", "up.*"}, wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ndjson := strings.Join([]string{`{"results":[]}`, `{"status":"success","has_more":false}`}, "\n") + "\n"
			root, _, _ := newSearchTestRoot(t, ndjson)
			root.SetArgs(tc.args)
			err := root.Execute()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSearchLabelNamesCmd_NoTermFallsBackToAlphaSort(t *testing.T) {
	ndjson := strings.Join([]string{`{"results":[]}`, `{"status":"success","has_more":false}`}, "\n") + "\n"

	root, _, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-label-names", "-d", "prom-uid", "--metric", "up"})
	require.NoError(t, root.Execute())

	_, query := captured()
	assert.Equal(t, "alpha", query.Get("sort_by"), "sort_by=score requires a term; omitting TERM should fall back to alpha")
}

func TestSearchLabelNamesCmd_ExplicitScoreSortWithoutTermIsRespected(t *testing.T) {
	ndjson := strings.Join([]string{`{"results":[]}`, `{"status":"success","has_more":false}`}, "\n") + "\n"

	root, _, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-label-names", "-d", "prom-uid", "--metric", "up", "--sort-by", "score"})
	require.NoError(t, root.Execute())

	_, query := captured()
	assert.Equal(t, "score", query.Get("sort_by"), "an explicit --sort-by=score must not be silently overridden")
}

func TestSearchLabelValuesCmd_NoTermAllowedWithMetric(t *testing.T) {
	ndjson := strings.Join([]string{`{"results":[]}`, `{"status":"success","has_more":false}`}, "\n") + "\n"

	root, _, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-label-values", "job", "-d", "prom-uid", "--metric", "up"})
	require.NoError(t, root.Execute())

	_, query := captured()
	assert.Equal(t, []string{`{__name__="up"}`}, query["match[]"])
	assert.Equal(t, "alpha", query.Get("sort_by"))
}

// TestSearchLabelValuesCmd_LabelAloneListsEveryValue proves LABEL alone is
// sufficient for search-label-values — unlike label-names/metric-names,
// LABEL already scopes the request to a bounded set of values, so no TERM
// or metric scope is required on top of it.
func TestSearchLabelValuesCmd_LabelAloneListsEveryValue(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"value":"prometheus"},{"value":"node"}]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	root, _, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-label-values", "job", "-d", "prom-uid"})
	require.NoError(t, root.Execute())

	_, query := captured()
	assert.Empty(t, query["search[]"], "no fuzzy term should be sent")
	assert.Empty(t, query["match[]"], "no metric scope should be sent")
	assert.Equal(t, "alpha", query.Get("sort_by"), "sort_by=score requires a term; omitting it should fall back to alpha")
}

// TestSearchLabelValuesCmd_SortDirAllowedWithNoTerm proves --sort-dir is
// accepted when TERM is omitted: sort_by silently downgrades from its
// score default to alpha before the sort-dir/sort-by=score incompatibility
// is checked, so the two are never in conflict here.
func TestSearchLabelValuesCmd_SortDirAllowedWithNoTerm(t *testing.T) {
	ndjson := strings.Join([]string{`{"results":[]}`, `{"status":"success","has_more":false}`}, "\n") + "\n"

	root, _, captured := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-label-values", "job", "-d", "prom-uid", "--sort-dir", "dsc"})
	require.NoError(t, root.Execute())

	_, query := captured()
	assert.Equal(t, "alpha", query.Get("sort_by"))
	assert.Equal(t, "dsc", query.Get("sort_dir"))
}

func TestSearchMetricNamesCmd_FeatureNotEnabled(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootdata" {
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"feature_not_enabled"}`))
	}))
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeSearchTestConfig(t, srv.URL))

	root := &cobra.Command{Use: "test"}
	root.AddCommand(dsprometheus.SearchMetricNamesCmd(loader))

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"search-metric-names", "up", "-d", "prom-uid"})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "experimental")
}

// TestSearchMetricNamesCmd_JSONKeysAreSnakeCase proves the output envelope
// and item fields use gcx's snake_case convention (not Go's default
// PascalCase), and that an absent Warnings array serializes as an omitted
// key rather than "null" — both were review findings on an earlier version
// of this envelope.
func TestSearchMetricNamesCmd_JSONKeysAreSnakeCase(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"name":"up","score":95,"type":"gauge","help":"1 if up","unit":"1"}]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	root, stdout, _ := newSearchTestRoot(t, ndjson)
	root.SetArgs([]string{"search-metric-names", "up", "-d", "prom-uid", "-o", "json", "--include-score", "--include-metadata"})
	require.NoError(t, root.Execute())

	out := stdout.String()
	assert.Contains(t, out, `"results"`)
	assert.Contains(t, out, `"name": "up"`)
	assert.Contains(t, out, `"score": 95`)
	assert.Contains(t, out, `"type": "gauge"`)
	assert.Contains(t, out, `"help": "1 if up"`)
	assert.Contains(t, out, `"unit": "1"`)
	assert.NotContains(t, out, "Results")
	assert.NotContains(t, out, "Name")
	assert.NotContains(t, out, "Score")
	assert.NotContains(t, out, `"warnings"`, "an absent warnings array must be omitted, not serialized as null")
	assert.NotContains(t, out, "null")
}

// TestSearchMetricNamesCmd_ExposesHasMoreAsListMeta proves a truncated
// server response (has_more=true) surfaces as list_meta in the output.
func TestSearchMetricNamesCmd_ExposesHasMoreAsListMeta(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"name":"up"}]}`,
		`{"status":"success","has_more":true}`,
	}, "\n") + "\n"

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootdata" {
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		_, _ = w.Write([]byte(ndjson))
	}))
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeSearchTestConfig(t, srv.URL))

	root := &cobra.Command{Use: "test"}
	root.AddCommand(dsprometheus.SearchMetricNamesCmd(loader))

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"search-metric-names", "up", "-d", "prom-uid", "-o", "json", "--limit", "1"})

	require.NoError(t, root.Execute())

	assert.Contains(t, stdout.String(), `"list_meta"`)
	assert.Contains(t, stdout.String(), `"truncated": true`)
	assert.NotContains(t, stdout.String(), `"HasMore"`)
	assert.NotEmpty(t, stderr.String(), "expected a truncation hint on stderr")
}

// TestSearchMetricNamesCmd_CompleteResultSetHasNoListMeta proves a complete
// result set (has_more=false) carries no list_meta and emits no stderr hint.
func TestSearchMetricNamesCmd_CompleteResultSetHasNoListMeta(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"name":"up"}]}`,
		`{"status":"success","has_more":false}`,
	}, "\n") + "\n"

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootdata" {
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		_, _ = w.Write([]byte(ndjson))
	}))
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeSearchTestConfig(t, srv.URL))

	root := &cobra.Command{Use: "test"}
	root.AddCommand(dsprometheus.SearchMetricNamesCmd(loader))

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"search-metric-names", "up", "-d", "prom-uid", "-o", "json"})

	require.NoError(t, root.Execute())

	assert.NotContains(t, stdout.String(), "list_meta")
	assert.Empty(t, stderr.String())
}

// TestSearchMetricNamesCmd_EmitsWarningsToStderr proves each server-reported
// warning (e.g. per-tenant limit clamping) is emitted as a stderr
// diagnostic, not silently dropped.
func TestSearchMetricNamesCmd_EmitsWarningsToStderr(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"results":[{"name":"up"}]}`,
		`{"status":"success","has_more":false,"warnings":["limit reached"]}`,
	}, "\n") + "\n"

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootdata" {
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		_, _ = w.Write([]byte(ndjson))
	}))
	defer srv.Close()

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeSearchTestConfig(t, srv.URL))

	root := &cobra.Command{Use: "test"}
	root.AddCommand(dsprometheus.SearchMetricNamesCmd(loader))

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"search-metric-names", "up", "-d", "prom-uid", "-o", "json"})

	require.NoError(t, root.Execute())

	assert.Contains(t, stderr.String(), "limit reached")
}
