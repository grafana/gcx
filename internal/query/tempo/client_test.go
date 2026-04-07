package tempo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func testClient(t *testing.T, handler http.Handler) *tempo.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	client, err := tempo.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	require.NoError(t, err)
	_, _ = w.Write(data)
}

func TestSearch(t *testing.T) {
	tests := []struct {
		name      string
		req       tempo.SearchRequest
		handler   http.HandlerFunc
		wantCount int
		wantErr   bool
	}{
		{
			name: "basic search with all params",
			req: tempo.SearchRequest{
				Query: `{resource.service.name="myservice"}`,
				Start: time.Unix(1700000000, 0),
				End:   time.Unix(1700003600, 0),
				Limit: 5,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/api/datasources/proxy/uid/tempo-ds/api/search")
				assert.Equal(t, `{resource.service.name="myservice"}`, r.URL.Query().Get("q"))
				assert.Equal(t, "1700000000", r.URL.Query().Get("start"))
				assert.Equal(t, "1700003600", r.URL.Query().Get("end"))
				assert.Equal(t, "5", r.URL.Query().Get("limit"))
				writeJSON(t, w, tempo.SearchResponse{
					Traces: []tempo.SearchTrace{
						{TraceID: "abc123", RootServiceName: "svc", RootTraceName: "GET /", DurationMs: 42},
						{TraceID: "def456", RootServiceName: "svc2", RootTraceName: "POST /api", DurationMs: 100},
					},
				})
			},
			wantCount: 2,
		},
		{
			name: "empty query omits q param",
			req:  tempo.SearchRequest{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Empty(t, r.URL.Query().Get("q"))
				assert.Empty(t, r.URL.Query().Get("start"))
				assert.Empty(t, r.URL.Query().Get("end"))
				assert.Empty(t, r.URL.Query().Get("limit"))
				writeJSON(t, w, tempo.SearchResponse{Traces: nil})
			},
			wantCount: 0,
		},
		{
			name: "server error",
			req:  tempo.SearchRequest{Query: "bad"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, tc.handler)
			resp, err := client.Search(context.Background(), "tempo-ds", tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, resp.Traces, tc.wantCount)
		})
	}
}

func TestGetTrace(t *testing.T) {
	tests := []struct {
		name    string
		req     tempo.GetTraceRequest
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "basic get trace",
			req:  tempo.GetTraceRequest{TraceID: "abc123def456"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/api/v2/traces/abc123def456")
				assert.Empty(t, r.Header.Get("Accept"))
				writeJSON(t, w, tempo.GetTraceResponse{
					Trace: map[string]any{"resourceSpans": []any{}},
				})
			},
		},
		{
			name: "LLM format sets accept header",
			req:  tempo.GetTraceRequest{TraceID: "abc123", LLMFormat: true},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tempo.AcceptLLM, r.Header.Get("Accept"))
				writeJSON(t, w, tempo.GetTraceResponse{
					Trace: map[string]any{"summary": "trace summary"},
				})
			},
		},
		{
			name: "with time range",
			req: tempo.GetTraceRequest{
				TraceID: "trace1",
				Start:   time.Unix(1700000000, 0),
				End:     time.Unix(1700003600, 0),
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "1700000000", r.URL.Query().Get("start"))
				assert.Equal(t, "1700003600", r.URL.Query().Get("end"))
				writeJSON(t, w, tempo.GetTraceResponse{})
			},
		},
		{
			name: "server error",
			req:  tempo.GetTraceRequest{TraceID: "missing"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("trace not found"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, tc.handler)
			resp, err := client.GetTrace(context.Background(), "tempo-ds", tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, resp)
		})
	}
}

func TestTags(t *testing.T) {
	tests := []struct {
		name    string
		req     tempo.TagsRequest
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "with scope and query",
			req:  tempo.TagsRequest{Scope: "resource", Query: `{status=error}`},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/api/v2/search/tags")
				assert.Equal(t, "resource", r.URL.Query().Get("scope"))
				assert.Equal(t, `{status=error}`, r.URL.Query().Get("q"))
				writeJSON(t, w, tempo.TagsResponse{
					Scopes: []tempo.TagScope{
						{Name: "resource", Tags: []string{"service.name", "host.name"}},
					},
				})
			},
		},
		{
			name: "empty params",
			req:  tempo.TagsRequest{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Empty(t, r.URL.Query().Get("scope"))
				assert.Empty(t, r.URL.Query().Get("q"))
				writeJSON(t, w, tempo.TagsResponse{
					Scopes: []tempo.TagScope{
						{Name: "resource", Tags: []string{"service.name"}},
						{Name: "span", Tags: []string{"http.method"}},
					},
				})
			},
		},
		{
			name: "server error",
			req:  tempo.TagsRequest{},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("bad request"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, tc.handler)
			resp, err := client.Tags(context.Background(), "tempo-ds", tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, resp.Scopes)
		})
	}
}

func TestTagValues(t *testing.T) {
	tests := []struct {
		name    string
		req     tempo.TagValuesRequest
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "tag with scope prepended",
			req:  tempo.TagValuesRequest{Tag: "service.name", Scope: "resource", Query: `{}`},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				// scope + "." + tag = "resource.service.name"
				assert.Contains(t, r.URL.Path, "/api/v2/search/tag/resource.service.name/values")
				assert.Equal(t, `{}`, r.URL.Query().Get("q"))
				writeJSON(t, w, tempo.TagValuesResponse{
					TagValues: []tempo.TagValue{
						{Type: "string", Value: "frontend"},
						{Type: "string", Value: "backend"},
					},
				})
			},
		},
		{
			name: "tag already has scope prefix",
			req:  tempo.TagValuesRequest{Tag: "resource.service.name", Scope: "resource"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Should NOT double-prefix
				assert.Contains(t, r.URL.Path, "/api/v2/search/tag/resource.service.name/values")
				writeJSON(t, w, tempo.TagValuesResponse{
					TagValues: []tempo.TagValue{{Type: "string", Value: "myservice"}},
				})
			},
		},
		{
			name: "no scope",
			req:  tempo.TagValuesRequest{Tag: "status"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "/api/v2/search/tag/status/values")
				assert.Empty(t, r.URL.Query().Get("q"))
				writeJSON(t, w, tempo.TagValuesResponse{
					TagValues: []tempo.TagValue{{Type: "string", Value: "ok"}},
				})
			},
		},
		{
			name: "server error",
			req:  tempo.TagValuesRequest{Tag: "bad"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("error"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, tc.handler)
			resp, err := client.TagValues(context.Background(), "tempo-ds", tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, resp.TagValues)
		})
	}
}

func TestMetricsRange(t *testing.T) {
	tests := []struct {
		name    string
		req     tempo.MetricsRequest
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "range query with step",
			req: tempo.MetricsRequest{
				Query: `{} | rate()`,
				Start: time.Unix(1700000000, 0),
				End:   time.Unix(1700003600, 0),
				Step:  "60s",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/api/metrics/query_range")
				assert.Equal(t, `{} | rate()`, r.URL.Query().Get("query"))
				assert.Equal(t, "1700000000", r.URL.Query().Get("start"))
				assert.Equal(t, "1700003600", r.URL.Query().Get("end"))
				assert.Equal(t, "60s", r.URL.Query().Get("step"))
				writeJSON(t, w, tempo.MetricsResponse{
					Series: []tempo.MetricsSeries{
						{
							Labels: []tempo.MetricsLabel{{Key: "service", Value: map[string]any{"stringValue": "web"}}},
							Samples: []tempo.MetricsSample{
								{TimestampMs: "1700000000000", Value: 42.5},
							},
						},
					},
				})
			},
		},
		{
			name: "step omitted when empty",
			req:  tempo.MetricsRequest{Query: `{} | rate()`},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Empty(t, r.URL.Query().Get("step"))
				writeJSON(t, w, tempo.MetricsResponse{})
			},
		},
		{
			name: "server error",
			req:  tempo.MetricsRequest{Query: "bad"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("invalid query"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, tc.handler)
			resp, err := client.MetricsRange(context.Background(), "tempo-ds", tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, resp)
			assert.False(t, resp.Instant)
		})
	}
}

func TestMetricsInstant(t *testing.T) {
	tests := []struct {
		name    string
		req     tempo.MetricsRequest
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "instant query uses query path not query_range",
			req: tempo.MetricsRequest{
				Query: `{} | count_over_time()`,
				Start: time.Unix(1700000000, 0),
				End:   time.Unix(1700003600, 0),
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/api/metrics/query")
				assert.NotContains(t, r.URL.Path, "query_range")
				assert.Equal(t, `{} | count_over_time()`, r.URL.Query().Get("query"))
				val := float64(99)
				writeJSON(t, w, tempo.MetricsResponse{
					Series: []tempo.MetricsSeries{
						{
							Labels:      []tempo.MetricsLabel{{Key: "service", Value: map[string]any{"stringValue": "api"}}},
							TimestampMs: "1700003600000",
							Value:       &val,
						},
					},
				})
			},
		},
		{
			name: "server error",
			req:  tempo.MetricsRequest{Query: "bad"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("error"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, tc.handler)
			resp, err := client.MetricsInstant(context.Background(), "tempo-ds", tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, resp)
			assert.True(t, resp.Instant)
		})
	}
}

func TestValidateTagScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		wantErr bool
	}{
		{name: "empty scope is valid", scope: "", wantErr: false},
		{name: "resource is valid", scope: "resource", wantErr: false},
		{name: "span is valid", scope: "span", wantErr: false},
		{name: "event is valid", scope: "event", wantErr: false},
		{name: "link is valid", scope: "link", wantErr: false},
		{name: "instrumentation is valid", scope: "instrumentation", wantErr: false},
		{name: "invalid scope", scope: "bogus", wantErr: true},
		{name: "partial match is invalid", scope: "res", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tempo.ValidateTagScope(tc.scope)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid tag scope")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
