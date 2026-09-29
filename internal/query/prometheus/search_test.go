package prometheus_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeNDJSON writes each frame as its own JSON line, matching the search
// API's streamed response shape (batches, then a trailer).
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

// TestClient_Search_FeatureNotEnabled pins each server's real disabled
// response to the enable hint, and proves other failures don't get it.
func TestClient_Search_FeatureNotEnabled(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantHint bool
	}{
		{
			name:     "Mimir 404 feature_not_enabled",
			status:   http.StatusNotFound,
			body:     `{"status":"error","errorType":"feature_not_enabled","error":"the experimental search API is not enabled"}`,
			wantHint: true,
		},
		{
			// Prometheus maps errorType unavailable to HTTP 500 by default.
			name:     "Prometheus 500 search API disabled",
			status:   http.StatusInternalServerError,
			body:     `{"status":"error","errorType":"unavailable","error":"search API disabled"}`,
			wantHint: true,
		},
		{
			name:   "bare 404 from a server without the endpoint",
			status: http.StatusNotFound,
			body:   "404 page not found\n",
		},
		{
			// Same errorType and status as the disabled case, so only the
			// message may trigger the hint.
			name:   "Prometheus 500 TSDB not ready",
			status: http.StatusInternalServerError,
			body:   `{"status":"error","errorType":"unavailable","error":"TSDB not ready"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			_, err := newTestClient(t, srv.URL).SearchMetricNames(context.Background(), "prom", prometheus.SearchOptions{Limit: 50})
			require.Error(t, err)

			// Flagged experimental so the CLI's route-absent handling
			// applies to the bare-404 case.
			var apiErr *queryerror.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.True(t, apiErr.Experimental)
			assert.Equal(t, tc.status, apiErr.StatusCode)

			if tc.wantHint {
				assert.Contains(t, err.Error(), "search API is not enabled on this server")
				assert.Contains(t, err.Error(), "--enable-feature=search-api")
				assert.Contains(t, err.Error(), "-querier.experimental-search-api-enabled")
			} else {
				assert.NotContains(t, err.Error(), "not enabled on this server")
			}
		})
	}
}

// TestClient_Search_LimitZeroRejectedByPrometheus proves Prometheus's
// rejection of limit=0 (which Mimir accepts as unlimited) gets a hint, and
// only when limit 0 was actually sent.
func TestClient_Search_LimitZeroRejectedByPrometheus(t *testing.T) {
	const body = `{"status":"error","errorType":"bad_data","error":"invalid limit \"0\": must be a positive integer"}`

	tests := []struct {
		name     string
		limit    int
		wantHint bool
	}{
		{name: "limit 0", limit: 0, wantHint: true},
		{name: "positive limit", limit: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			_, err := newTestClient(t, srv.URL).SearchMetricNames(context.Background(), "prom", prometheus.SearchOptions{Limit: tc.limit})
			require.Error(t, err)
			assert.Contains(t, err.Error(), `invalid limit "0"`, "the upstream error must stay in the chain")
			if tc.wantHint {
				assert.Contains(t, err.Error(), "only supported by Mimir")
			} else {
				assert.NotContains(t, err.Error(), "only supported by Mimir")
			}
		})
	}
}

// TestClient_Search_Warnings pins where each server puts warnings:
// Prometheus on the first batch (the trailer repeats only a changed set),
// Mimir on the trailer. All are surfaced, each once, in arrival order.
func TestClient_Search_Warnings(t *testing.T) {
	tests := []struct {
		name   string
		frames []string
		want   []string
	}{
		{
			name: "Prometheus: first batch only",
			frames: []string{
				`{"results":[{"name":"up"}],"warnings":["partial result: store unavailable"]}`,
				`{"results":[{"name":"go_goroutines"}]}`,
				`{"status":"success","has_more":false}`,
			},
			want: []string{"partial result: store unavailable"},
		},
		{
			name: "Prometheus: trailer re-sends a grown set",
			frames: []string{
				`{"results":[{"name":"up"}],"warnings":["a"]}`,
				`{"status":"success","has_more":false,"warnings":["a","b"]}`,
			},
			want: []string{"a", "b"},
		},
		{
			name: "Mimir: trailer only",
			frames: []string{
				`{"results":[{"name":"up"}]}`,
				`{"status":"success","has_more":true,"warnings":["limit reached"]}`,
			},
			want: []string{"limit reached"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeNDJSON(w, tc.frames...)
			}))
			defer srv.Close()

			resp, err := newTestClient(t, srv.URL).SearchMetricNames(context.Background(), "prom", prometheus.SearchOptions{})
			require.NoError(t, err)
			assert.Equal(t, tc.want, resp.Warnings)
		})
	}
}

// TestDecodeSearchStream_IncompleteStream proves a stream without a trailer
// fails with an error saying why, instead of returning results that may be
// incomplete.
func TestDecodeSearchStream_IncompleteStream(t *testing.T) {
	const (
		batch   = `{"results":[{"name":"up"},{"name":"go_goroutines"}]}` + "\n"
		trailer = `{"status":"success","has_more":false}` + "\n"
	)

	tests := []struct {
		name    string
		body    string
		limit   int64
		wantErr string
	}{
		{
			name:    "no trailer",
			body:    batch,
			limit:   1 << 20,
			wantErr: "search stream ended after 2 results without a completion trailer",
		},
		{
			name:    "cut mid-line",
			body:    batch + `{"results":[{"na`,
			limit:   1 << 20,
			wantErr: "search stream ended after 2 results without a completion trailer",
		},
		{
			name:    "size cap hit",
			body:    batch + batch + trailer,
			limit:   int64(len(batch)) + 10,
			wantErr: "search response exceeded the",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := prometheus.DecodeSearchStream(strings.NewReader(tc.body), tc.limit)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}

	t.Run("stream ending exactly at the cap is complete", func(t *testing.T) {
		body := batch + trailer
		n, _, _, err := prometheus.DecodeSearchStream(strings.NewReader(body), int64(len(body)))
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	})
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
