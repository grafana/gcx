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
	"github.com/grafana/gcx/internal/queryerror"
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

func boolPtr(b bool) *bool { return &b }

func intPtr(i int) *int { return &i }

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

func TestSearchParsesServiceStats(t *testing.T) {
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"traces":[{"traceID":"abc","rootServiceName":"svc","serviceStats":{"svc":{"spanCount":5},"gateway":{"spanCount":2,"errorCount":1}}}]}`))
	}
	client := testClient(t, http.HandlerFunc(handler))
	resp, err := client.Search(context.Background(), "tempo-ds", tempo.SearchRequest{Query: "{}"})
	require.NoError(t, err)
	require.Len(t, resp.Traces, 1)
	stats := resp.Traces[0].ServiceStats
	require.NotNil(t, stats)
	assert.Equal(t, 5, stats["svc"].SpanCount)
	assert.Equal(t, 1, stats["gateway"].ErrorCount)
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
		{
			name: "V2 spanset filter sends keep_hierarchy/match_depth/ancestor_depth",
			req: tempo.GetTraceRequest{
				TraceID:       "trace1",
				Query:         "{ status = error }",
				KeepHierarchy: true,
				MatchDepth:    2,
				AncestorDepth: -1,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "{ status = error }", r.URL.Query().Get("q"))
				assert.Equal(t, "true", r.URL.Query().Get("keep_hierarchy"))
				assert.Equal(t, "2", r.URL.Query().Get("match_depth"))
				assert.Equal(t, "-1", r.URL.Query().Get("ancestor_depth"))
				writeJSON(t, w, tempo.GetTraceResponse{})
			},
		},
		{
			name: "empty query omits spanset filter params",
			req:  tempo.GetTraceRequest{TraceID: "trace1"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.False(t, r.URL.Query().Has("q"))
				assert.False(t, r.URL.Query().Has("keep_hierarchy"))
				assert.False(t, r.URL.Query().Has("match_depth"))
				assert.False(t, r.URL.Query().Has("ancestor_depth"))
				writeJSON(t, w, tempo.GetTraceResponse{})
			},
		},
		{
			name: "span pruning enabled with tuning params",
			req: tempo.GetTraceRequest{
				TraceID:                   "trace1",
				SpanPruning:               boolPtr(true),
				SpanPruningGroupBy:        "db.*,http.method",
				SpanPruningMinSpans:       intPtr(3),
				SpanPruningMaxParentDepth: intPtr(2),
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "true", r.URL.Query().Get("span_pruning"))
				assert.Equal(t, "db.*,http.method", r.URL.Query().Get("span_pruning_group_by"))
				assert.Equal(t, "3", r.URL.Query().Get("span_pruning_min_spans"))
				assert.Equal(t, "2", r.URL.Query().Get("span_pruning_max_parent_depth"))
				writeJSON(t, w, tempo.GetTraceResponse{})
			},
		},
		{
			name: "unset span pruning omits all pruning params",
			req:  tempo.GetTraceRequest{TraceID: "trace1"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.False(t, r.URL.Query().Has("span_pruning"))
				assert.False(t, r.URL.Query().Has("span_pruning_group_by"))
				assert.False(t, r.URL.Query().Has("span_pruning_min_spans"))
				assert.False(t, r.URL.Query().Has("span_pruning_max_parent_depth"))
				writeJSON(t, w, tempo.GetTraceResponse{})
			},
		},
		{
			name: "span pruning explicitly disabled",
			req: tempo.GetTraceRequest{
				TraceID:     "trace1",
				SpanPruning: boolPtr(false),
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "false", r.URL.Query().Get("span_pruning"))
				writeJSON(t, w, tempo.GetTraceResponse{})
			},
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
		name        string
		req         tempo.TagValuesRequest
		handler     http.HandlerFunc
		wantErr     bool
		wantLLM     bool
		wantByType  bool
		wantMetrics bool
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
			name:        "LLM format sets accept header and parses compact response",
			req:         tempo.TagValuesRequest{Tag: "service.name", Scope: "resource", LLMFormat: true},
			wantLLM:     true,
			wantByType:  true,
			wantMetrics: true,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tempo.AcceptLLM, r.Header.Get("Accept"))
				assert.Contains(t, r.URL.Path, "/api/v2/search/tag/resource.service.name/values")
				w.Header().Set("Content-Type", "application/vnd.grafana.llm+json")
				_, err := w.Write([]byte(`{"tagValues":{"string":["frontend","backend"],"int":[500]},"metrics":{"inspectedBytes":123}}`))
				assert.NoError(t, err)
			},
		},
		{
			name:        "LLM content type marks response as LLM format",
			req:         tempo.TagValuesRequest{Tag: "service.name", Scope: "resource"},
			wantLLM:     true,
			wantByType:  true,
			wantMetrics: true,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Empty(t, r.Header.Get("Accept"))
				assert.Contains(t, r.URL.Path, "/api/v2/search/tag/resource.service.name/values")
				w.Header().Set("Content-Type", "application/vnd.grafana.llm+json; charset=utf-8")
				_, err := w.Write([]byte(`{"tagValues":{"string":["frontend","backend"]},"metrics":{"inspectedBytes":123}}`))
				assert.NoError(t, err)
			},
		},
		{
			name:    "LLM request with standard response keeps compact output preference",
			req:     tempo.TagValuesRequest{Tag: "service.name", Scope: "resource", LLMFormat: true},
			wantLLM: true,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tempo.AcceptLLM, r.Header.Get("Accept"))
				assert.Contains(t, r.URL.Path, "/api/v2/search/tag/resource.service.name/values")
				writeJSON(t, w, tempo.TagValuesResponse{
					TagValues: []tempo.TagValue{{Type: "string", Value: "frontend"}},
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
			if tc.wantLLM {
				assert.True(t, resp.LLMFormat)
				if tc.wantByType {
					assert.NotEmpty(t, resp.TagValuesByType)
				}
				if tc.wantMetrics {
					assert.EqualValues(t, 123, resp.Metrics["inspectedBytes"])
				}

				encoded, err := json.Marshal(resp)
				require.NoError(t, err)
				assert.Contains(t, string(encoded), `"tagValues":{"`)
			}
		})
	}
}

func TestTagValuesResponseMarshalJSON_LLMCompactsStandardValues(t *testing.T) {
	resp := tempo.TagValuesResponse{
		LLMFormat: true,
		TagValues: []tempo.TagValue{
			{Type: "string", Value: "frontend"},
			{Type: "string", Value: "backend"},
			{Type: "int", Value: 500},
		},
	}

	encoded, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.JSONEq(t, `{"tagValues":{"int":[500],"string":["frontend","backend"]}}`, string(encoded))
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

func TestDiff(t *testing.T) {
	tests := []struct {
		name             string
		req              tempo.DiffRequest
		handler          http.HandlerFunc
		wantErr          bool
		wantCloudOnly    bool
		wantExperimental bool
	}{
		{
			name: "posts base and compare trace ids without time bounds",
			req:  tempo.DiffRequest{BaseTraceID: "aaa", CompareTraceID: "bbb"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "/api/datasources/proxy/uid/tempo-ds/api/v2/traces/diff")
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var payload map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				base, _ := payload["base"].(map[string]any)
				compare, _ := payload["compare"].(map[string]any)
				assert.Equal(t, "aaa", base["traceId"])
				assert.Equal(t, "bbb", compare["traceId"])
				_, hasStart := base["start"]
				assert.False(t, hasStart)
				writeJSON(t, w, map[string]any{"summary": map[string]any{"verdict": "regression"}})
			},
		},
		{
			name: "includes time bounds when set",
			req:  tempo.DiffRequest{BaseTraceID: "a", CompareTraceID: "b", Start: time.Unix(1700000000, 0), End: time.Unix(1700003600, 0)},
			handler: func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				base, _ := payload["base"].(map[string]any)
				assert.EqualValues(t, 1700000000, base["start"])
				assert.EqualValues(t, 1700003600, base["end"])
				writeJSON(t, w, map[string]any{"summary": map[string]any{}})
			},
		},
		{
			// A truly absent route returns Go's default "404 page not found".
			// The client marks it Cloud-only + experimental via WithAvailability
			// so the fail package can render an actionable message.
			name: "absent endpoint surfaces cloud-only experimental error",
			req:  tempo.DiffRequest{BaseTraceID: "a", CompareTraceID: "b"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("404 page not found"))
			},
			wantErr:          true,
			wantCloudOnly:    true,
			wantExperimental: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, tc.handler)
			resp, err := client.Diff(context.Background(), "tempo-ds", tc.req)
			if tc.wantErr {
				require.Error(t, err)
				apiErr := &queryerror.APIError{}
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, tc.wantCloudOnly, apiErr.CloudOnly)
				assert.Equal(t, tc.wantExperimental, apiErr.Experimental)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Contains(t, resp, "summary")
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
