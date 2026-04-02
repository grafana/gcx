package logs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	logs "github.com/grafana/gcx/internal/providers/logs/adaptive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, server *httptest.Server) *logs.Client {
	t.Helper()
	return logs.NewClient(server.URL, 12345, "test-token", server.Client())
}

// writeJSON encodes v as JSON to w.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write(data)
}

func TestClient_ListExemptions(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantCount int
		wantErr   bool
	}{
		{
			name: "unwraps result envelope",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/adaptive-logs/exemptions", r.URL.Path)
				writeJSON(w, map[string]any{
					"result": []logs.Exemption{
						{ID: "ex-1", StreamSelector: `{app="foo"}`},
						{ID: "ex-2", StreamSelector: `{app="bar"}`},
					},
				})
			},
			wantCount: 2,
		},
		{
			name: "empty result returns empty slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]any{"result": []logs.Exemption{}})
			},
			wantCount: 0,
		},
		{
			name: "null result returns empty slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]any{})
			},
			wantCount: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]any{"error": "internal server error"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			exemptions, err := client.ListExemptions(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, exemptions, tt.wantCount)
		})
	}
}

func TestAPIError_TypedError(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantCode    int
		wantMessage string
	}{
		{
			name: "extracts error field from JSON body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]any{"error": "invalid stream selector"})
			},
			wantCode:    400,
			wantMessage: "invalid stream selector",
		},
		{
			name: "extracts message field from JSON body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				writeJSON(w, map[string]any{"message": "access denied"})
			},
			wantCode:    403,
			wantMessage: "access denied",
		},
		{
			name: "sanitizes non-JSON response body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("service unavailable"))
			},
			wantCode:    503,
			wantMessage: "received non-JSON error response body",
		},
		{
			name: "empty body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantCode: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			_, err := client.ListExemptions(t.Context())
			require.Error(t, err)

			var apiErr *logs.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, tt.wantCode, apiErr.StatusCode)
			if tt.wantMessage != "" {
				assert.Contains(t, apiErr.Message, tt.wantMessage)
			}
		})
	}
}

func TestClient_CreateExemption(t *testing.T) {
	tests := []struct {
		name    string
		input   *logs.Exemption
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name:  "returns created exemption (envelope)",
			input: &logs.Exemption{StreamSelector: `{app="critical"}`},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var received logs.Exemption
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Fatal(err)
				}
				assert.Equal(t, `{app="critical"}`, received.StreamSelector)

				w.WriteHeader(http.StatusCreated)
				writeJSON(w, map[string]any{
					"result": logs.Exemption{ID: "new-id", StreamSelector: `{app="critical"}`},
				})
			},
			wantID: "new-id",
		},
		{
			name:  "unwraps result envelope",
			input: &logs.Exemption{StreamSelector: `{app="wrapped"}`},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				writeJSON(w, map[string]any{
					"result": logs.Exemption{ID: "wrapped-id", StreamSelector: `{app="wrapped"}`},
				})
			},
			wantID: "wrapped-id",
		},
		{
			name:  "server error",
			input: &logs.Exemption{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]any{"error": "invalid exemption"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			created, err := client.CreateExemption(t.Context(), tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, created.ID)
		})
	}
}

func TestClient_UpdateExemption(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		input   *logs.Exemption
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name:  "returns updated exemption (envelope)",
			id:    "ex-1",
			input: &logs.Exemption{StreamSelector: `{app="updated"}`},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "/adaptive-logs/exemptions/ex-1", r.URL.Path)
				writeJSON(w, map[string]any{
					"result": logs.Exemption{ID: "ex-1", StreamSelector: `{app="updated"}`},
				})
			},
			wantID: "ex-1",
		},
		{
			name:  "unwraps result envelope",
			id:    "ex-2",
			input: &logs.Exemption{StreamSelector: `{app="wrapped"}`},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				writeJSON(w, map[string]any{
					"result": logs.Exemption{ID: "ex-2", StreamSelector: `{app="wrapped"}`},
				})
			},
			wantID: "ex-2",
		},
		{
			name:  "not found",
			id:    "missing",
			input: &logs.Exemption{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]any{"error": "not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			updated, err := client.UpdateExemption(t.Context(), tt.id, tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, updated.ID)
		})
	}
}

func TestClient_DeleteExemption(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success 204",
			id:   "ex-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/adaptive-logs/exemptions/ex-1", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "success 200",
			id:   "ex-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "not found",
			id:   "missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]any{"error": "exemption not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.DeleteExemption(t.Context(), tt.id)

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
		name      string
		handler   http.HandlerFunc
		wantCount int
		wantErr   bool
		checkFn   func(t *testing.T, recs []logs.LogRecommendation)
	}{
		{
			name: "populates Pattern via Label() when empty",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/adaptive-logs/recommendations", r.URL.Path)
				writeJSON(w, []logs.LogRecommendation{
					{
						Tokens:              []string{"msg=", "error"},
						ConfiguredDropRate:  0.1,
						RecommendedDropRate: 0.5,
					},
					{
						Pattern:             "existing pattern",
						ConfiguredDropRate:  0.2,
						RecommendedDropRate: 0.3,
					},
				})
			},
			wantCount: 2,
			checkFn: func(t *testing.T, recs []logs.LogRecommendation) {
				t.Helper()
				assert.Equal(t, "msg=error", recs[0].Pattern, "should set Pattern from tokens via Label()")
				assert.Equal(t, "existing pattern", recs[1].Pattern, "should preserve non-empty Pattern")
			},
		},
		{
			name: "populates Pattern from segments when no tokens",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []logs.LogRecommendation{
					{
						Segments: map[string]logs.Segment{
							"service": {Volume: 100},
							"env":     {Volume: 50},
						},
					},
				})
			},
			wantCount: 1,
			checkFn: func(t *testing.T, recs []logs.LogRecommendation) {
				t.Helper()
				assert.Equal(t, "{env, service}", recs[0].Pattern)
			},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
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
			recs, err := client.ListRecommendations(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, recs, tt.wantCount)
			if tt.checkFn != nil {
				tt.checkFn(t, recs)
			}
		})
	}
}

func TestClient_ListSegments(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantCount int
		wantErr   bool
	}{
		{
			name: "returns bare array",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/adaptive-logs/segments", r.URL.Path)
				writeJSON(w, []logs.LogSegment{
					{ID: "seg-1", Name: "production"},
					{ID: "seg-2", Name: "staging"},
				})
			},
			wantCount: 2,
		},
		{
			name: "null returns empty slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("null"))
			},
			wantCount: 0,
		},
		{
			name: "empty array",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []logs.LogSegment{})
			},
			wantCount: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]any{"error": "internal server error"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			segments, err := client.ListSegments(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, segments, tt.wantCount)
		})
	}
}

func TestClient_GetSegment(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name: "uses query param",
			id:   "seg-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/adaptive-logs/segment", r.URL.Path)
				assert.Equal(t, "seg-1", r.URL.Query().Get("segment"))
				writeJSON(w, logs.LogSegment{ID: "seg-1", Name: "production"})
			},
			wantID: "seg-1",
		},
		{
			name: "not found",
			id:   "missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]any{"error": "segment not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			segment, err := client.GetSegment(t.Context(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, segment.ID)
		})
	}
}

func TestClient_CreateSegment(t *testing.T) {
	tests := []struct {
		name    string
		input   *logs.LogSegment
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name:  "returns created segment",
			input: &logs.LogSegment{Name: "new-segment", Selector: `{env="prod"}`},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/adaptive-logs/segment", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var received logs.LogSegment
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Fatal(err)
				}
				assert.Equal(t, "new-segment", received.Name)

				w.WriteHeader(http.StatusCreated)
				writeJSON(w, logs.LogSegment{ID: "seg-new", Name: "new-segment"})
			},
			wantID: "seg-new",
		},
		{
			name:  "server error",
			input: &logs.LogSegment{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]any{"error": "invalid segment"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			created, err := client.CreateSegment(t.Context(), tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, created.ID)
		})
	}
}

func TestClient_UpdateSegment(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		input   *logs.LogSegment
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name:  "uses query param",
			id:    "seg-1",
			input: &logs.LogSegment{Name: "updated-segment"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "/adaptive-logs/segment", r.URL.Path)
				assert.Equal(t, "seg-1", r.URL.Query().Get("segment"))
				writeJSON(w, logs.LogSegment{ID: "seg-1", Name: "updated-segment"})
			},
			wantID: "seg-1",
		},
		{
			name:  "not found",
			id:    "missing",
			input: &logs.LogSegment{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]any{"error": "not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			updated, err := client.UpdateSegment(t.Context(), tt.id, tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, updated.ID)
		})
	}
}

func TestClient_DeleteSegment(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success 204",
			id:   "seg-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/adaptive-logs/segment", r.URL.Path)
				assert.Equal(t, "seg-1", r.URL.Query().Get("segment"))
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "success 200",
			id:   "seg-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "not found",
			id:   "missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]any{"error": "segment not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.DeleteSegment(t.Context(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}
