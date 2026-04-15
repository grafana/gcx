package traces_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	traces "github.com/grafana/gcx/internal/providers/traces/adaptive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(server *httptest.Server) *traces.Client {
	return traces.NewClient(server.URL, 42, "test-token", server.Client())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write(data)
}

// ---------------------------------------------------------------------------
// ListPolicies
// ---------------------------------------------------------------------------

func TestClient_ListPolicies(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantCount int
		wantErr   bool
	}{
		{
			name: "success with items",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/adaptive-traces/api/v1/policies", r.URL.Path)
				assert.Equal(t, "Basic NDI6dGVzdC10b2tlbg==", r.Header.Get("Authorization"))
				writeJSON(w, []traces.Policy{
					{ID: "policy-1", Type: "probabilistic", Name: "Policy 1"},
					{ID: "policy-2", Type: "rate_limiting", Name: "Policy 2"},
				})
			},
			wantCount: 2,
		},
		{
			name: "null response returns empty slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("null"))
			},
			wantCount: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "internal server error"})
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestClient(srv)
			got, err := client.ListPolicies(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tc.wantCount)
		})
	}
}

// ---------------------------------------------------------------------------
// GetPolicy
// ---------------------------------------------------------------------------

func TestClient_GetPolicy(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name: "success",
			id:   "policy-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/adaptive-traces/api/v1/policies/policy-1", r.URL.Path)
				writeJSON(w, traces.Policy{ID: "policy-1", Type: "probabilistic", Name: "Policy 1"})
			},
			wantID: "policy-1",
		},
		{
			name: "url-escaped ID",
			id:   "policy/with/slashes",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/adaptive-traces/api/v1/policies/policy%2Fwith%2Fslashes", r.URL.RawPath)
				writeJSON(w, traces.Policy{ID: "policy/with/slashes", Name: "Escaped"})
			},
			wantID: "policy/with/slashes",
		},
		{
			name: "not found",
			id:   "missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]string{"error": "not found"})
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestClient(srv)
			got, err := client.GetPolicy(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, got.ID)
		})
	}
}

// ---------------------------------------------------------------------------
// CreatePolicy
// ---------------------------------------------------------------------------

func TestClient_CreatePolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  *traces.Policy
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name:   "success",
			policy: &traces.Policy{Type: "probabilistic", Name: "New Policy"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/adaptive-traces/api/v1/policies", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, traces.Policy{ID: "new-id", Type: "probabilistic", Name: "New Policy"})
			},
			wantID: "new-id",
		},
		{
			name:   "server error",
			policy: &traces.Policy{Type: "probabilistic", Name: "Bad Policy"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "invalid policy"})
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestClient(srv)
			got, err := client.CreatePolicy(context.Background(), tc.policy)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, got.ID)
		})
	}
}

// ---------------------------------------------------------------------------
// UpdatePolicy
// ---------------------------------------------------------------------------

func TestClient_UpdatePolicy(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		policy  *traces.Policy
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name:   "success",
			id:     "policy-1",
			policy: &traces.Policy{ID: "policy-1", Type: "probabilistic", Name: "Updated"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "/adaptive-traces/api/v1/policies/policy-1", r.URL.Path)
				writeJSON(w, traces.Policy{ID: "policy-1", Type: "probabilistic", Name: "Updated"})
			},
			wantID: "policy-1",
		},
		{
			name:   "server error",
			id:     "policy-x",
			policy: &traces.Policy{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestClient(srv)
			got, err := client.UpdatePolicy(context.Background(), tc.id, tc.policy)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, got.ID)
		})
	}
}

// ---------------------------------------------------------------------------
// DeletePolicy
// ---------------------------------------------------------------------------

func TestClient_DeletePolicy(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success with 200",
			id:   "policy-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/adaptive-traces/api/v1/policies/policy-1", r.URL.Path)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "success with 204",
			id:   "policy-2",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "not found",
			id:   "missing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestClient(srv)
			err := client.DeletePolicy(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// ---------------------------------------------------------------------------
// GetConfig
// ---------------------------------------------------------------------------

func TestClient_GetConfig(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    *traces.ReadonlyTenantConfig
		wantErr bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/adaptive-traces/api/v1/config", r.URL.Path)
				assert.Equal(t, "Basic NDI6dGVzdC10b2tlbg==", r.Header.Get("Authorization"))
				writeJSON(w, traces.ReadonlyTenantConfig{
					DisableAnomalyPolicies:          true,
					SpanNameSemconvTransformEnabled: false,
					SpanNameSemconvVersion:          "v1.2.0",
					AnomalyRateLimitBytesPerSec:     1024.5,
				})
			},
			want: &traces.ReadonlyTenantConfig{
				DisableAnomalyPolicies:          true,
				SpanNameSemconvTransformEnabled: false,
				SpanNameSemconvVersion:          "v1.2.0",
				AnomalyRateLimitBytesPerSec:     1024.5,
			},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "internal server error"})
			},
			wantErr: true,
		},
		{
			name: "not found",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestClient(srv)
			got, err := client.GetConfig(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// UpdateConfig
// ---------------------------------------------------------------------------

func TestClient_UpdateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *traces.TenantConfig
		handler http.HandlerFunc
		want    *traces.TenantConfig
		wantErr bool
	}{
		{
			name: "success",
			cfg: &traces.TenantConfig{
				DisableAnomalyPolicies:          true,
				SpanNameSemconvTransformEnabled: true,
				SpanNameSemconvVersion:          "v1.3.0",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "/adaptive-traces/api/v1/config", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var cfg traces.TenantConfig
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&cfg))
				assert.True(t, cfg.DisableAnomalyPolicies)
				assert.True(t, cfg.SpanNameSemconvTransformEnabled)
				assert.Equal(t, "v1.3.0", cfg.SpanNameSemconvVersion)

				writeJSON(w, cfg)
			},
			want: &traces.TenantConfig{
				DisableAnomalyPolicies:          true,
				SpanNameSemconvTransformEnabled: true,
				SpanNameSemconvVersion:          "v1.3.0",
			},
		},
		{
			name: "server error",
			cfg:  &traces.TenantConfig{},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "bad request"})
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestClient(srv)
			got, err := client.UpdateConfig(context.Background(), tc.cfg)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// ListRecommendations
// ---------------------------------------------------------------------------

func TestClient_ListRecommendations(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantCount int
		wantErr   bool
	}{
		{
			name: "success with items",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/adaptive-traces/api/v1/recommendations", r.URL.Path)
				writeJSON(w, []traces.Recommendation{
					{ID: "rec-1", Message: "Sample less", Tags: []string{"sampling"}},
					{ID: "rec-2", Message: "Enable rate limiting", Tags: []string{}},
				})
			},
			wantCount: 2,
		},
		{
			name: "null response returns empty slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("null"))
			},
			wantCount: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestClient(srv)
			got, err := client.ListRecommendations(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tc.wantCount)
		})
	}
}

// ---------------------------------------------------------------------------
// ApplyRecommendation
// ---------------------------------------------------------------------------

//nolint:dupl // Similar test pattern for similar API, acceptable duplication.
func TestClient_ApplyRecommendation(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			id:   "rec-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/adaptive-traces/api/v1/recommendations/rec-1/apply", r.URL.Path)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "success with 204",
			id:   "rec-2",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/adaptive-traces/api/v1/recommendations/rec-2/apply", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "server error",
			id:   "rec-bad",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestClient(srv)
			err := client.ApplyRecommendation(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// ---------------------------------------------------------------------------
// DismissRecommendation
// ---------------------------------------------------------------------------

//nolint:dupl // Similar test pattern for similar API, acceptable duplication.
func TestClient_DismissRecommendation(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			id:   "rec-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/adaptive-traces/api/v1/recommendations/rec-1/dismiss", r.URL.Path)
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "success with 204",
			id:   "rec-2",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/adaptive-traces/api/v1/recommendations/rec-2/dismiss", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "server error",
			id:   "rec-bad",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := newTestClient(srv)
			err := client.DismissRecommendation(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
