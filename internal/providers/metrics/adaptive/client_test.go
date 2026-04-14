package metrics_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	metrics "github.com/grafana/gcx/internal/providers/metrics/adaptive"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, server *httptest.Server) *metrics.Client {
	t.Helper()
	return metrics.NewClient(context.Background(), server.URL, 12345, "test-token", nil)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func TestErrRuleNotFound_WrapsAdapterErrNotFound(t *testing.T) {
	assert.ErrorIs(t, metrics.ErrRuleNotFound, adapter.ErrNotFound,
		"ErrRuleNotFound must wrap adapter.ErrNotFound so push upsert falls through to Create")
}

func TestErrSegmentNotFound_WrapsAdapterErrNotFound(t *testing.T) {
	assert.ErrorIs(t, metrics.ErrSegmentNotFound, adapter.ErrNotFound,
		"ErrSegmentNotFound must wrap adapter.ErrNotFound so push upsert falls through to Create")
}

func TestErrExemptionNotFound_WrapsAdapterErrNotFound(t *testing.T) {
	assert.ErrorIs(t, metrics.ErrExemptionNotFound, adapter.ErrNotFound,
		"ErrExemptionNotFound must wrap adapter.ErrNotFound so push upsert falls through to Create")
}

// ---------------------------------------------------------------------------
// Segments
// ---------------------------------------------------------------------------

func TestClient_ListSegments(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "returns segments",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/aggregations/rules/segments", r.URL.Path)
				writeJSON(w, []map[string]any{
					{"id": "seg-1", "name": "production", "selector": `{env="prod"}`},
					{"id": "seg-2", "name": "staging", "selector": `{env="staging"}`},
				})
			},
			wantLen: 2,
		},
		{
			name: "returns empty list for null response",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("null"))
			},
			wantLen: 0,
		},
		{
			name: "returns empty list for empty array",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, []any{})
			},
			wantLen: 0,
		},
		{
			name: "propagates 4xx error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("unauthorized"))
			},
			wantErr: true,
		},
		{
			name: "propagates 5xx error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("server error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			segments, err := client.ListSegments(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, segments, tt.wantLen)
		})
	}
}

func TestClient_GetSegment(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		handler    http.HandlerFunc
		wantName   string
		wantErr    bool
		wantErr404 bool
	}{
		{
			name: "returns matching segment",
			id:   "seg-1",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, []map[string]any{
					{"id": "seg-1", "name": "production", "selector": `{env="prod"}`},
					{"id": "seg-2", "name": "staging", "selector": `{env="staging"}`},
				})
			},
			wantName: "production",
		},
		{
			name: "returns ErrSegmentNotFound when ID not in list",
			id:   "nonexistent",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, []map[string]any{
					{"id": "seg-1", "name": "production"},
				})
			},
			wantErr404: true,
		},
		{
			name: "returns ErrSegmentNotFound for empty list",
			id:   "seg-1",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, []any{})
			},
			wantErr404: true,
		},
		{
			name: "propagates list error",
			id:   "seg-1",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			segment, err := client.GetSegment(context.Background(), tt.id)

			if tt.wantErr404 {
				require.ErrorIs(t, err, metrics.ErrSegmentNotFound)
				return
			}
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantName, segment.Name)
			assert.Equal(t, tt.id, segment.ID)
		})
	}
}

func TestClient_CreateSegment(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "creates segment and returns result",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/aggregations/rules/segments", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var seg metrics.MetricSegment
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&seg))
				assert.Equal(t, "production", seg.Name)

				writeJSON(w, map[string]any{
					"id":       "seg-new",
					"name":     "production",
					"selector": `{env="prod"}`,
				})
			},
		},
		{
			name: "propagates error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("invalid selector"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			seg := &metrics.MetricSegment{Name: "production", Selector: `{env="prod"}`}
			created, err := client.CreateSegment(context.Background(), seg)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "seg-new", created.ID)
			assert.Equal(t, "production", created.Name)
		})
	}
}

func TestClient_UpdateSegment(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantErr404 bool
	}{
		{
			name: "updates segment and returns merged result",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "seg-1", r.URL.Query().Get("segment"))
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "returns ErrSegmentNotFound on 404",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr404: true,
		},
		{
			name: "propagates other errors",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("server error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			seg := &metrics.MetricSegment{Name: "updated"}
			result, err := client.UpdateSegment(context.Background(), "seg-1", seg)

			if tt.wantErr404 {
				require.ErrorIs(t, err, metrics.ErrSegmentNotFound)
				return
			}
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "seg-1", result.ID)
			assert.Equal(t, "updated", result.Name)
		})
	}
}

func TestClient_DeleteSegment(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
		errMsg  string
	}{
		{
			name: "deletes segment",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "seg-1", r.URL.Query().Get("segment"))
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "returns conflict error for dependent data",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte("has rules"))
			},
			wantErr: true,
			errMsg:  "dependent rules or exemptions",
		},
		{
			name: "propagates other errors",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("server error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.DeleteSegment(context.Background(), "seg-1")

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

// ---------------------------------------------------------------------------
// Exemptions
// ---------------------------------------------------------------------------

func TestClient_ListExemptions(t *testing.T) {
	tests := []struct {
		name    string
		segment string
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "returns exemptions",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/v1/recommendations/exemptions", r.URL.Path)
				writeJSON(w, map[string]any{
					"result": []map[string]any{
						{"id": "ex-1", "metric": "http_requests_total"},
						{"id": "ex-2", "metric": "up"},
					},
				})
			},
			wantLen: 2,
		},
		{
			name:    "passes segment query param",
			segment: "my-segment",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "my-segment", r.URL.Query().Get("segment"))
				writeJSON(w, map[string]any{"result": []any{}})
			},
			wantLen: 0,
		},
		{
			name: "returns empty list for null result",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"result": nil})
			},
			wantLen: 0,
		},
		{
			name: "propagates error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("server error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			exemptions, err := client.ListExemptions(context.Background(), tt.segment)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, exemptions, tt.wantLen)
		})
	}
}

func TestClient_ListSegmentedExemptions(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "returns grouped exemptions",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/v1/recommendations/segmented_exemptions", r.URL.Path)
				writeJSON(w, []map[string]any{
					{
						"segment":    map[string]any{"id": "seg-1", "name": "prod"},
						"exemptions": []map[string]any{{"id": "ex-1", "metric": "m1"}},
					},
					{
						"segment":    map[string]any{"id": "seg-2", "name": "staging"},
						"exemptions": []map[string]any{{"id": "ex-2", "metric": "m2"}, {"id": "ex-3", "metric": "m3"}},
					},
				})
			},
			wantLen: 2,
		},
		{
			name: "returns empty list for null response",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("null"))
			},
			wantLen: 0,
		},
		{
			name: "propagates error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			entries, err := client.ListSegmentedExemptions(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, entries, tt.wantLen)
		})
	}
}

func TestClient_GetExemption(t *testing.T) {
	tests := []struct {
		name       string
		segment    string
		handler    http.HandlerFunc
		wantMetric string
		wantErr    bool
		wantErr404 bool
	}{
		{
			name: "returns exemption",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/v1/recommendations/exemptions/ex-1", r.URL.Path)
				writeJSON(w, map[string]any{
					"result": map[string]any{"id": "ex-1", "metric": "http_requests_total"},
				})
			},
			wantMetric: "http_requests_total",
		},
		{
			name:    "passes segment query param",
			segment: "my-segment",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "my-segment", r.URL.Query().Get("segment"))
				writeJSON(w, map[string]any{
					"result": map[string]any{"id": "ex-1", "metric": "up"},
				})
			},
			wantMetric: "up",
		},
		{
			name: "returns ErrExemptionNotFound on 404",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr404: true,
		},
		{
			name: "returns error for null result",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"result": nil})
			},
			wantErr: true,
		},
		{
			name: "propagates other errors",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("server error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			exemption, err := client.GetExemption(context.Background(), "ex-1", tt.segment)

			if tt.wantErr404 {
				require.ErrorIs(t, err, metrics.ErrExemptionNotFound)
				return
			}
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantMetric, exemption.Metric)
		})
	}
}

func TestClient_CreateExemption(t *testing.T) {
	tests := []struct {
		name    string
		segment string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "creates exemption and returns result",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v1/recommendations/exemptions", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var e metrics.MetricExemption
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&e))
				assert.Equal(t, "http_requests_total", e.Metric)

				writeJSON(w, map[string]any{
					"result": map[string]any{"id": "ex-new", "metric": "http_requests_total"},
				})
			},
		},
		{
			name:    "passes segment query param",
			segment: "my-segment",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "my-segment", r.URL.Query().Get("segment"))
				writeJSON(w, map[string]any{
					"result": map[string]any{"id": "ex-new", "metric": "http_requests_total"},
				})
			},
		},
		{
			name: "returns error for null result",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"result": nil})
			},
			wantErr: true,
		},
		{
			name: "propagates error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("invalid"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			e := &metrics.MetricExemption{Metric: "http_requests_total", MatchType: "exact"}
			created, err := client.CreateExemption(context.Background(), e, tt.segment)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "ex-new", created.ID)
		})
	}
}

func TestClient_UpdateExemption(t *testing.T) {
	tests := []struct {
		name       string
		segment    string
		handler    http.HandlerFunc
		wantErr    bool
		wantErr404 bool
	}{
		{
			name: "updates exemption and returns merged result",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "/v1/recommendations/exemptions/ex-1", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:    "passes segment query param",
			segment: "my-segment",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "my-segment", r.URL.Query().Get("segment"))
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "returns ErrExemptionNotFound on 404",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr404: true,
		},
		{
			name: "propagates other errors",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("server error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			e := &metrics.MetricExemption{Metric: "http_requests_total"}
			result, err := client.UpdateExemption(context.Background(), "ex-1", e, tt.segment)

			if tt.wantErr404 {
				require.ErrorIs(t, err, metrics.ErrExemptionNotFound)
				return
			}
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "ex-1", result.ID)
			assert.Equal(t, "http_requests_total", result.Metric)
		})
	}
}

func TestClient_DeleteExemption(t *testing.T) {
	tests := []struct {
		name    string
		segment string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "deletes exemption",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/v1/recommendations/exemptions/ex-1", r.URL.Path)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:    "passes segment query param",
			segment: "my-segment",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "my-segment", r.URL.Query().Get("segment"))
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "propagates error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("server error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.DeleteExemption(context.Background(), "ex-1", tt.segment)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestClient_ListRules(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantLen  int
		wantEtag string
		wantErr  bool
	}{
		{
			name: "returns rules and ETag",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/aggregations/rules", r.URL.Path)
				w.Header().Set("Etag", `"abc123"`)
				writeJSON(w, []map[string]any{
					{"metric": "http_requests_total", "drop_labels": []string{"pod"}, "aggregations": []string{"sum"}},
					{"metric": "up"},
				})
			},
			wantLen:  2,
			wantEtag: `"abc123"`,
		},
		{
			name: "passes segment query param",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "my-segment", r.URL.Query().Get("segment"))
				w.Header().Set("Etag", `"seg"`)
				writeJSON(w, []any{})
			},
			wantLen:  0,
			wantEtag: `"seg"`,
		},
		{
			name: "returns empty list with ETag",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				w.Header().Set("Etag", `"empty"`)
				writeJSON(w, []any{})
			},
			wantLen:  0,
			wantEtag: `"empty"`,
		},
		{
			name: "propagates 4xx error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("unauthorized"))
			},
			wantErr: true,
		},
		{
			name: "propagates 5xx error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("server error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			segment := ""
			if tt.name == "passes segment query param" {
				segment = "my-segment"
			}
			rules, etag, err := client.ListRules(context.Background(), segment)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, rules, tt.wantLen)
			assert.Equal(t, tt.wantEtag, etag)
		})
	}
}

func TestClient_GetRule(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantErr404 bool
	}{
		{
			name: "returns rule",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/aggregations/rule/http_requests_total", r.URL.Path)
				writeJSON(w, map[string]any{
					"metric":       "http_requests_total",
					"drop_labels":  []string{"pod"},
					"aggregations": []string{"sum"},
				})
			},
		},
		{
			name: "returns ErrRuleNotFound on 404",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr404: true,
		},
		{
			name: "propagates other errors",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			rule, err := client.GetRule(context.Background(), "http_requests_total", "")

			if tt.wantErr404 {
				require.ErrorIs(t, err, metrics.ErrRuleNotFound)
				return
			}
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "http_requests_total", rule.Metric)
		})
	}
}

func TestClient_CreateRule(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "creates rule and returns ETag",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/aggregations/rule/http_requests_total", r.URL.Path)
				assert.Equal(t, `"existing"`, r.Header.Get("If-Match"))

				var rule metrics.MetricRule
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&rule))
				assert.Equal(t, "http_requests_total", rule.Metric)

				w.Header().Set("Etag", `"new1"`)
				w.WriteHeader(http.StatusCreated)
			},
		},
		{
			name: "propagates error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("invalid"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			rule := metrics.MetricRule{Metric: "http_requests_total", DropLabels: []string{"pod"}}
			etag, err := client.CreateRule(context.Background(), rule, `"existing"`, "")

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, `"new1"`, etag)
		})
	}
}

func TestClient_UpdateRule(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
		want412 bool
	}{
		{
			name: "updates rule with If-Match",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, `"old"`, r.Header.Get("If-Match"))
				w.Header().Set("Etag", `"new"`)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "returns ErrPreconditionFailed on 412",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusPreconditionFailed)
			},
			want412: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			rule := metrics.MetricRule{Metric: "http_requests_total"}
			_, err := client.UpdateRule(context.Background(), rule, `"old"`, "")

			if tt.want412 {
				require.ErrorIs(t, err, metrics.ErrPreconditionFailed)
				return
			}
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_DeleteRule(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
		want412 bool
	}{
		{
			name: "deletes rule with If-Match",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/aggregations/rule/up", r.URL.Path)
				assert.Equal(t, `"etag1"`, r.Header.Get("If-Match"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "returns ErrPreconditionFailed on 412",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusPreconditionFailed)
			},
			want412: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.DeleteRule(context.Background(), "up", `"etag1"`, "")

			if tt.want412 {
				require.ErrorIs(t, err, metrics.ErrPreconditionFailed)
				return
			}
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_SyncRules(t *testing.T) {
	tests := []struct {
		name    string
		rules   []metrics.MetricRule
		etag    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "sends rules with If-Match header",
			etag: `"abc123"`,
			rules: []metrics.MetricRule{
				{Metric: "http_requests_total", DropLabels: []string{"pod"}, Aggregations: []string{"sum"}},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/aggregations/rules", r.URL.Path)
				assert.Equal(t, `"abc123"`, r.Header.Get("If-Match"))

				var rules []metrics.MetricRule
				if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
					t.Fatal(err)
				}
				assert.Len(t, rules, 1)
				assert.Equal(t, "http_requests_total", rules[0].Metric)

				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:  "sends empty ETag when not set",
			etag:  "",
			rules: []metrics.MetricRule{{Metric: "up"}},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Empty(t, r.Header.Get("If-Match"))
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:  "propagates 4xx error",
			etag:  `"stale"`,
			rules: []metrics.MetricRule{},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte("etag mismatch"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.SyncRules(context.Background(), tt.rules, tt.etag, "")

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestClient_ValidateRules(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantErrs []string
		wantErr  bool
	}{
		{
			name: "returns empty on valid rules",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/aggregations/check-rules", r.URL.Path)
				writeJSON(w, []string{})
			},
			wantErrs: []string{},
		},
		{
			name: "returns validation errors",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, []string{"invalid aggregation: bad_fn", "unknown match type: partial"})
			},
			wantErrs: []string{"invalid aggregation: bad_fn", "unknown match type: partial"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			errs, err := client.ValidateRules(context.Background(), []metrics.MetricRule{{Metric: "up"}}, "")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantErrs, errs)
		})
	}
}

func TestClient_ListRecommendations(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "returns recommendations",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/aggregations/recommendations", r.URL.Path)
				assert.Equal(t, "true", r.URL.Query().Get("verbose"))
				writeJSON(w, []map[string]any{
					{"metric": "http_requests_total", "recommended_action": "update", "drop_labels": []string{"pod", "container"}, "aggregations": []string{"sum", "count"}},
					{"metric": "node_cpu_seconds_total", "recommended_action": "add", "aggregations": []string{"sum"}},
				})
			},
			wantLen: 2,
		},
		{
			name: "passes action filter params",
			handler: func(w http.ResponseWriter, r *http.Request) {
				actions := r.URL.Query()["action"]
				assert.ElementsMatch(t, []string{"add", "update"}, actions)
				writeJSON(w, []any{})
			},
			wantLen: 0,
		},
		{
			name: "returns empty recommendations",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, []any{})
			},
			wantLen: 0,
		},
		{
			name: "propagates 5xx error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("server error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			actions := []string{}
			if tt.name == "passes action filter params" {
				actions = []string{"add", "update"}
			}
			recs, err := client.ListRecommendations(context.Background(), "", actions)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, recs, tt.wantLen)
		})
	}
}

func TestClient_ListRecommendedRules(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantLen     int
		wantVersion string
		wantErr     bool
	}{
		{
			name: "returns rules and Rules-Version",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "false", r.URL.Query().Get("verbose"))
				w.Header().Set("Rules-Version", `"v42"`)
				writeJSON(w, []map[string]any{
					{"metric": "http_requests_total"},
				})
			},
			wantLen:     1,
			wantVersion: `"v42"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			rules, version, err := client.ListRecommendedRules(context.Background(), "")

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, rules, tt.wantLen)
			assert.Equal(t, tt.wantVersion, version)
		})
	}
}
