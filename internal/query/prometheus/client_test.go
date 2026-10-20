package prometheus_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestQuery_StepBudget(t *testing.T) {
	tests := []struct {
		name       string
		rangeMs    int64
		step       time.Duration
		instant    bool
		wantBudget int64
		wantErr    string
	}{
		{name: "omitted step", rangeMs: 3600000},
		{name: "aligned range", rangeMs: 3600000, step: time.Second, wantBudget: 3600},
		{name: "unaligned range rounds up", rangeMs: 3600001, step: time.Second, wantBudget: 3601},
		{name: "step larger than range", rangeMs: 1000, step: 2 * time.Second, wantBudget: 1},
		{name: "minimum step", rangeMs: 10, step: time.Millisecond, wantBudget: 10},
		{name: "sub-millisecond step", rangeMs: 10, step: time.Microsecond, wantErr: "must be at least 1ms"},
		{name: "negative step", rangeMs: 10, step: -time.Second, wantErr: "must be at least 1ms"},
		{name: "large budget", rangeMs: 100001000, step: time.Second, wantBudget: 100001},
		{name: "unaligned large budget", rangeMs: 100000001, step: time.Second, wantBudget: 100001},
		{name: "ceiling does not overflow", rangeMs: math.MaxInt64, step: time.Second, wantBudget: 9223372036854776},
		{name: "reversed range", rangeMs: -1, step: time.Second, wantErr: "range must be non-negative"},
		{name: "instant unchanged", rangeMs: 3600000, step: time.Second, instant: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body struct {
				Queries []map[string]any `json:"queries"`
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, http.MethodPost, r.Method)
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
			}))
			defer server.Close()

			client := newTestClient(t, server.URL)
			resp, err := client.Query(context.Background(), "prom-uid", prometheus.QueryRequest{
				Query: "vector(1)", Start: time.UnixMilli(0), End: time.UnixMilli(tt.rangeMs),
				Step: tt.step, Instant: tt.instant,
			})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Zero(t, calls, "invalid requests must fail before network I/O")
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Len(t, body.Queries, 1)
			query := body.Queries[0]
			if tt.wantBudget == 0 {
				assert.NotContains(t, query, "maxDataPoints")
				assert.EqualValues(t, 60000, query["intervalMs"])
				assert.Nil(t, resp.Resolution)
			} else {
				assert.EqualValues(t, tt.wantBudget, query["maxDataPoints"])
				assert.EqualValues(t, tt.step.Milliseconds(), query["intervalMs"])
				require.NotNil(t, resp.Resolution)
				assert.Equal(t, []int64{}, resp.Resolution.ObservedStepMs)
			}
		})
	}
}

func TestQuery_RangeOverflow(t *testing.T) {
	client := &prometheus.Client{}
	_, err := client.Query(context.Background(), "prom-uid", prometheus.QueryRequest{
		Query: "vector(1)", Start: time.UnixMilli(-1), End: time.UnixMilli(math.MaxInt64),
		Step: time.Second,
	})
	require.ErrorContains(t, err, "fit in int64 milliseconds")
}

func TestQuery_FallsBackOn403(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/api/ds/query" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"forbidden"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	client, err := prometheus.NewClient(cfg)
	require.NoError(t, err)

	_, err = client.Query(context.Background(), "prom-uid", prometheus.QueryRequest{Query: "up"})
	require.NoError(t, err)

	require.Len(t, paths, 2)
	assert.Contains(t, paths[0], "/apis/query.grafana.app/v0alpha1/namespaces/default/query")
	assert.Equal(t, "/api/ds/query", paths[1])
}

func TestClient_Labels_Match(t *testing.T) {
	var (
		capturedPath  string
		capturedQuery url.Values
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":["__name__","instance","job"]}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	resp, err := client.Labels(context.Background(), "prom-uid", []string{"http_requests_total", `{job="api"}`})
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/prom-uid/resources/api/v1/labels", capturedPath)
	assert.Equal(t, []string{"http_requests_total", `{job="api"}`}, capturedQuery["match[]"])
	require.NotNil(t, resp)
	assert.Equal(t, []string{"__name__", "instance", "job"}, resp.Data)
}

func TestClient_Labels_NoMatch(t *testing.T) {
	var capturedRawQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.Labels(context.Background(), "prom-uid", nil)
	require.NoError(t, err)

	assert.Empty(t, capturedRawQuery)
}

func TestClient_LabelValues_Match(t *testing.T) {
	var (
		capturedPath  string
		capturedQuery url.Values
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":["api","worker"]}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	resp, err := client.LabelValues(context.Background(), "prom-uid", "job", []string{"http_requests_total"})
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/prom-uid/resources/api/v1/label/job/values", capturedPath)
	assert.Equal(t, []string{"http_requests_total"}, capturedQuery["match[]"])
	require.NotNil(t, resp)
	assert.Equal(t, []string{"api", "worker"}, resp.Data)
}

func TestClient_LabelValues_NoMatch(t *testing.T) {
	var capturedRawQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.LabelValues(context.Background(), "prom-uid", "job", nil)
	require.NoError(t, err)

	assert.Empty(t, capturedRawQuery)
}

func TestBuildPathsEscapeDatasourceUID(t *testing.T) {
	c := &prometheus.Client{}
	uid := "uid/../admin"
	escapedUID := url.PathEscape(uid)

	tests := []struct {
		name string
		path string
	}{
		{"labels", c.BuildLabelsPath(uid)},
		{"labelValues", c.BuildLabelValuesPath(uid, "job")},
		{"metadata", c.BuildMetadataPath(uid)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(tt.path, uid) && !strings.Contains(tt.path, escapedUID) {
				t.Errorf("path contains unescaped UID: %s", tt.path)
			}
			if !strings.Contains(tt.path, escapedUID) {
				t.Errorf("path missing escaped UID %q: %s", escapedUID, tt.path)
			}
		})
	}
}
