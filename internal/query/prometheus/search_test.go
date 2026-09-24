package prometheus_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeNDJSON writes each frame as its own JSON line, matching the Mimir
// search API's streamed response shape (batches, then a trailer).
func writeNDJSON(w http.ResponseWriter, frames ...string) {
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	_, _ = w.Write([]byte(strings.Join(frames, "\n") + "\n"))
}

func TestClient_SearchMetricNames(t *testing.T) {
	var (
		capturedPath  string
		capturedQuery url.Values
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()

		writeNDJSON(w,
			`{"results":[{"name":"up","score":95,"type":"gauge","help":"1 if up","unit":""}]}`,
			`{"results":[{"name":"go_goroutines","score":80}]}`,
			`{"status":"success","has_more":true,"warnings":["limit reached"]}`,
		)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	resp, err := client.SearchMetricNames(context.Background(), "grafanacloud-prom", prometheus.SearchOptions{
		Search:          []string{"up", "go"},
		Match:           []string{`{job="api"}`},
		FuzzAlg:         "jarowinkler",
		FuzzThreshold:   50,
		SortBy:          "score",
		Limit:           50,
		IncludeScore:    true,
		IncludeMetadata: true,
	})
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/grafanacloud-prom/resources/api/v1/search/metric_names", capturedPath)
	assert.Equal(t, []string{"up", "go"}, capturedQuery["search[]"])
	assert.Equal(t, []string{`{job="api"}`}, capturedQuery["match[]"])
	assert.Equal(t, "jarowinkler", capturedQuery.Get("fuzz_alg"))
	assert.Equal(t, "50", capturedQuery.Get("fuzz_threshold"))
	assert.Equal(t, "score", capturedQuery.Get("sort_by"))
	assert.Equal(t, "50", capturedQuery.Get("limit"))
	assert.Equal(t, "true", capturedQuery.Get("include_score"))
	assert.Equal(t, "true", capturedQuery.Get("include_metadata"))

	require.NotNil(t, resp)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, "up", resp.Results[0].Name)
	assert.InDelta(t, 95.0, resp.Results[0].Score, 0)
	assert.Equal(t, "gauge", resp.Results[0].Type)
	assert.Equal(t, "1 if up", resp.Results[0].Help)
	assert.Equal(t, "go_goroutines", resp.Results[1].Name)
	assert.InDelta(t, 80.0, resp.Results[1].Score, 0)
	assert.True(t, resp.HasMore)
	assert.Equal(t, []string{"limit reached"}, resp.Warnings)
}

func TestClient_SearchMetricNames_OmitsUnsetOptionalParams(t *testing.T) {
	var capturedQuery url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query()
		writeNDJSON(w, `{"status":"success","has_more":false}`)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.SearchMetricNames(context.Background(), "prom", prometheus.SearchOptions{})
	require.NoError(t, err)

	for _, key := range []string{"search[]", "match[]", "case_sensitive", "fuzz_alg", "fuzz_threshold", "sort_by", "sort_dir", "include_score", "include_metadata"} {
		_, present := capturedQuery[key]
		assert.False(t, present, "expected %q to be omitted", key)
	}
	// limit is always sent (0 is a meaningful value for the server:
	// "unlimited"), so an unset Go zero value must not be indistinguishable
	// from an explicit request for it.
	assert.Equal(t, "0", capturedQuery.Get("limit"))
}

func TestClient_SearchMetricNames_CaseSensitiveFalse(t *testing.T) {
	var capturedQuery url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query()
		writeNDJSON(w, `{"status":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	caseSensitive := false
	_, err := client.SearchMetricNames(context.Background(), "prom", prometheus.SearchOptions{CaseSensitive: &caseSensitive})
	require.NoError(t, err)

	assert.Equal(t, "false", capturedQuery.Get("case_sensitive"))
}

// TestClient_SearchMetricNames_FractionalScore proves Score decodes as
// float64, not int: the real search API returns fuzzy-match scores with
// fractional values (e.g. jarowinkler similarity), which an int field would
// truncate or fail to decode.
func TestClient_SearchMetricNames_FractionalScore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeNDJSON(w,
			`{"results":[{"name":"up","score":92.5}]}`,
			`{"status":"success"}`,
		)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	resp, err := client.SearchMetricNames(context.Background(), "prom", prometheus.SearchOptions{})
	require.NoError(t, err)

	require.Len(t, resp.Results, 1)
	assert.InDelta(t, 92.5, resp.Results[0].Score, 0)
}

func TestClient_SearchLabelNames(t *testing.T) {
	var capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		writeNDJSON(w,
			`{"results":[{"name":"job"},{"name":"instance"}]}`,
			`{"status":"success","has_more":false}`,
		)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	resp, err := client.SearchLabelNames(context.Background(), "prom", prometheus.SearchOptions{Search: []string{"job"}})
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/prom/resources/api/v1/search/label_names", capturedPath)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, "job", resp.Results[0].Name)
	assert.Equal(t, "instance", resp.Results[1].Name)
	assert.False(t, resp.HasMore)
}

func TestClient_SearchLabelValues(t *testing.T) {
	var (
		capturedPath  string
		capturedQuery url.Values
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		writeNDJSON(w,
			`{"results":[{"value":"prometheus"},{"value":"node"}]}`,
			`{"status":"success","has_more":false}`,
		)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	resp, err := client.SearchLabelValues(context.Background(), "prom", "job", prometheus.SearchOptions{Search: []string{"pro"}})
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/prom/resources/api/v1/search/label_values", capturedPath)
	assert.Equal(t, "job", capturedQuery.Get("label"))
	require.Len(t, resp.Results, 2)
	assert.Equal(t, "prometheus", resp.Results[0].Value)
	assert.Equal(t, "node", resp.Results[1].Value)
}

func TestClient_Search_ErrorTrailer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeNDJSON(w,
			`{"results":[{"name":"up"}]}`,
			`{"status":"error","errorType":"timeout","error":"context deadline exceeded"}`,
		)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.SearchMetricNames(context.Background(), "prom", prometheus.SearchOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
	assert.Contains(t, err.Error(), "timeout")
}

func TestClient_Search_FeatureNotEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"feature_not_enabled"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.SearchMetricNames(context.Background(), "prom", prometheus.SearchOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "experimental")
	assert.Contains(t, err.Error(), "-querier.experimental-search-api-enabled")
}

func TestClient_Search_OtherHTTPErrorIsRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.SearchMetricNames(context.Background(), "prom", prometheus.SearchOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.NotContains(t, err.Error(), "experimental")
}

func TestClient_BuildSearchPathsEscapeUID(t *testing.T) {
	c := &prometheus.Client{}

	assert.Contains(t, c.BuildSearchMetricNamesPath("uid/../admin"), "uid%2F..%2Fadmin")
	assert.Contains(t, c.BuildSearchLabelNamesPath("uid/../admin"), "uid%2F..%2Fadmin")
	assert.Contains(t, c.BuildSearchLabelValuesPath("uid/../admin"), "uid%2F..%2Fadmin")
}
