package logs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/adaptive/logs"
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

func TestClient_GetExemption(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name: "success with plain ID",
			id:   "ex-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/adaptive-logs/exemptions/ex-1", r.URL.Path)
				writeJSON(w, logs.Exemption{ID: "ex-1", StreamSelector: `{app="foo"}`})
			},
			wantID: "ex-1",
		},
		{
			name: "URL-escapes ID with special chars",
			id:   "ex/special id",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/adaptive-logs/exemptions/ex%2Fspecial%20id", r.URL.RawPath)
				writeJSON(w, logs.Exemption{ID: "ex/special id", StreamSelector: `{app="foo"}`})
			},
			wantID: "ex/special id",
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
			exemption, err := client.GetExemption(t.Context(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, exemption.ID)
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
			name:  "returns created exemption",
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
				writeJSON(w, logs.Exemption{ID: "new-id", StreamSelector: `{app="critical"}`})
			},
			wantID: "new-id",
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
			name:  "returns updated exemption",
			id:    "ex-1",
			input: &logs.Exemption{StreamSelector: `{app="updated"}`},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "/adaptive-logs/exemptions/ex-1", r.URL.Path)
				writeJSON(w, logs.Exemption{ID: "ex-1", StreamSelector: `{app="updated"}`})
			},
			wantID: "ex-1",
		},
		{
			name:  "not found",
			id:    "missing",
			input: &logs.Exemption{},
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

func TestClient_ApplyRecommendations(t *testing.T) {
	tests := []struct {
		name    string
		input   []logs.LogRecommendation
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "POSTs full array",
			input: []logs.LogRecommendation{
				{Pattern: "pattern-1", ConfiguredDropRate: 0.5},
				{Pattern: "pattern-2", ConfiguredDropRate: 0.3},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/adaptive-logs/recommendations", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var received []logs.LogRecommendation
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Fatal(err)
				}
				assert.Len(t, received, 2)
				assert.Equal(t, "pattern-1", received[0].Pattern)
				assert.Equal(t, "pattern-2", received[1].Pattern)

				w.WriteHeader(http.StatusAccepted)
			},
		},
		{
			name:  "server error",
			input: []logs.LogRecommendation{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]any{"error": "bad request"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.ApplyRecommendations(t.Context(), tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}
