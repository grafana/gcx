package metrics_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/adaptive/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, server *httptest.Server) *metrics.Client {
	t.Helper()
	return metrics.NewClient(server.URL, 12345, "test-token", nil)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
			rules, etag, err := client.ListRules(context.Background())

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
				{MetricName: "http_requests_total", DropLabels: []string{"pod"}, Aggregations: []string{"sum"}},
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
				assert.Equal(t, "http_requests_total", rules[0].MetricName)

				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:  "sends empty ETag when not set",
			etag:  "",
			rules: []metrics.MetricRule{{MetricName: "up"}},
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
			err := client.SyncRules(context.Background(), tt.rules, tt.etag)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
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
				writeJSON(w, []map[string]any{
					{"metric": "http_requests_total", "drop_labels": []string{"pod", "container"}, "aggregations": []string{"sum", "count"}},
					{"metric": "node_cpu_seconds_total", "aggregations": []string{"sum"}},
				})
			},
			wantLen: 2,
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
			recs, err := client.ListRecommendations(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, recs, tt.wantLen)
		})
	}
}
