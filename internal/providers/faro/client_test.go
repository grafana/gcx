package faro_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/faro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *faro.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stack-123",
	}
	c, err := faro.NewClient(cfg)
	require.NoError(t, err)
	return c
}

func TestClient_List(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "returns converted FaroApp slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app", r.URL.Path)
				writeJSON(w, []map[string]any{
					{
						"id":   42,
						"name": "my-web-app",
						"extraLogLabels": []map[string]string{
							{"key": "team", "value": "frontend"},
						},
					},
					{
						"id":   43,
						"name": "my-other-app",
					},
				})
			},
			wantLen: 2,
		},
		{
			name: "returns empty list",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, []map[string]any{})
			},
			wantLen: 0,
		},
		{
			name: "propagates server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			result, err := c.List(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result, tt.wantLen)

			if tt.wantLen > 0 {
				assert.Equal(t, "42", result[0].ID)
				assert.Equal(t, "my-web-app", result[0].Name)
				assert.Equal(t, "frontend", result[0].ExtraLogLabels["team"])
			}
		})
	}
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name: "returns single converted FaroApp",
			id:   "42",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42", r.URL.Path)
				writeJSON(w, map[string]any{
					"id":   42,
					"name": "my-web-app",
				})
			},
			wantID: "42",
		},
		{
			name: "returns error on 404",
			id:   "999",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("not found"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			result, err := c.Get(t.Context(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, result.ID)
		})
	}
}

func TestClient_Create(t *testing.T) {
	t.Run("strips ExtraLogLabels and Settings from request body", func(t *testing.T) {
		var capturedBody map[string]any
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method == http.MethodPost {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &capturedBody)
				writeJSON(w, map[string]any{
					"id":   100,
					"name": "new-app",
				})
				return
			}
			// GET for list (re-fetch after create)
			writeJSON(w, []map[string]any{
				{
					"id":                 100,
					"name":               "new-app",
					"appKey":             "abc-key",
					"collectEndpointURL": "https://collect.example.com",
				},
			})
		}))
		defer server.Close()

		c := newTestClient(t, server)
		app := &faro.FaroApp{
			Name: "new-app",
			CORSOrigins: []faro.CORSOrigin{
				{URL: "https://example.com"},
			},
			ExtraLogLabels: map[string]string{
				"team": "frontend",
			},
			Settings: &faro.FaroAppSettings{
				GeolocationEnabled: true,
				GeolocationLevel:   "country",
			},
		}

		result, err := c.Create(t.Context(), app)
		require.NoError(t, err)

		// Verify ExtraLogLabels was stripped from request.
		assert.Nil(t, capturedBody["extraLogLabels"], "extraLogLabels should be stripped from create request")
		// Verify Settings was stripped from request.
		assert.Nil(t, capturedBody["settings"], "settings should be stripped from create request")

		// Re-fetched via list returns full details.
		assert.Equal(t, "100", result.ID)
		assert.Equal(t, "abc-key", result.AppKey)
		assert.Equal(t, "https://collect.example.com", result.CollectEndpointURL)
	})
}

func TestClient_Update(t *testing.T) {
	t.Run("strips Settings and includes ID in body", func(t *testing.T) {
		var capturedBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42", r.URL.Path)
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &capturedBody)
			writeJSON(w, map[string]any{
				"id":   42,
				"name": "updated-app",
			})
		}))
		defer server.Close()

		c := newTestClient(t, server)
		app := &faro.FaroApp{
			Name: "updated-app",
			CORSOrigins: []faro.CORSOrigin{
				{URL: "https://example.com"},
			},
			ExtraLogLabels: map[string]string{
				"team": "frontend",
			},
			Settings: &faro.FaroAppSettings{
				GeolocationEnabled: true,
			},
		}

		result, err := c.Update(t.Context(), "42", app)
		require.NoError(t, err)

		// Settings should be stripped.
		assert.Nil(t, capturedBody["settings"], "settings should be stripped from update request")
		// ID should be present in body.
		assert.InDelta(t, float64(42), capturedBody["id"], 0.01, "id should be in update request body")
		assert.Equal(t, "42", result.ID)
	})
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "returns nil on 204",
			id:   "42",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "returns error on 404",
			id:   "999",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			err := c.Delete(t.Context(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestClient_ListSourcemaps(t *testing.T) {
	tests := []struct {
		name    string
		appID   string
		limit   int
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name:  "default limit 100 with single page",
			appID: "42",
			limit: 0,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps", r.URL.Path)
				assert.Equal(t, "100", r.URL.Query().Get("limit"))
				assert.Empty(t, r.URL.Query().Get("page"))
				writeJSON(w, map[string]any{
					"bundles": []map[string]any{
						{"ID": "bundle-1", "Created": "2024-01-01T00:00:00Z", "Updated": "2024-01-02T00:00:00Z"},
						{"ID": "bundle-2", "Created": "2024-01-03T00:00:00Z", "Updated": "2024-01-04T00:00:00Z"},
					},
					"page": map[string]any{
						"hasNext":    false,
						"next":       "",
						"limit":      100,
						"totalItems": 2,
					},
				})
			},
			wantLen: 2,
		},
		{
			name:  "respects explicit limit without pagination",
			appID: "42",
			limit: 50,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps", r.URL.Path)
				assert.Equal(t, "50", r.URL.Query().Get("limit"))
				writeJSON(w, map[string]any{
					"bundles": []map[string]any{
						{"ID": "bundle-1", "Created": "2024-01-01T00:00:00Z", "Updated": "2024-01-02T00:00:00Z"},
					},
					"page": map[string]any{
						"hasNext":    true,
						"next":       "page2",
						"limit":      50,
						"totalItems": 2,
					},
				})
			},
			wantLen: 1,
		},
		{
			name:  "auto-pagination with limit=0",
			appID: "42",
			limit: 0,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps", r.URL.Path)
				assert.Equal(t, "100", r.URL.Query().Get("limit"))

				switch pageParam := r.URL.Query().Get("page"); pageParam {
				case "":
					// First page
					writeJSON(w, map[string]any{
						"bundles": []map[string]any{
							{"ID": "bundle-1", "Created": "2024-01-01T00:00:00Z", "Updated": "2024-01-02T00:00:00Z"},
						},
						"page": map[string]any{
							"hasNext":    true,
							"next":       "page2",
							"limit":      100,
							"totalItems": 2,
						},
					})
				case "page2":
					// Second page
					writeJSON(w, map[string]any{
						"bundles": []map[string]any{
							{"ID": "bundle-2", "Created": "2024-01-03T00:00:00Z", "Updated": "2024-01-04T00:00:00Z"},
						},
						"page": map[string]any{
							"hasNext":    false,
							"next":       "",
							"limit":      100,
							"totalItems": 2,
						},
					})
				default:
					http.Error(w, "unexpected page param: "+pageParam, http.StatusBadRequest)
				}
			},
			wantLen: 2,
		},
		{
			name:  "propagates server error",
			appID: "42",
			limit: 0,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			result, err := c.ListSourcemaps(t.Context(), tt.appID, tt.limit)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result, tt.wantLen)
		})
	}
}

func TestClient_DeleteSourcemaps(t *testing.T) {
	tests := []struct {
		name      string
		appID     string
		bundleIDs []string
		handler   http.HandlerFunc
		wantErr   bool
	}{
		{
			name:      "single bundle ID",
			appID:     "42",
			bundleIDs: []string{"bundle-1"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps/batch/bundle-1", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name:      "multiple bundle IDs",
			appID:     "42",
			bundleIDs: []string{"bundle-1", "bundle-2", "bundle-3"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps/batch/bundle-1,bundle-2,bundle-3", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name:      "server error",
			appID:     "42",
			bundleIDs: []string{"bundle-1"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			err := c.DeleteSourcemaps(t.Context(), tt.appID, tt.bundleIDs)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
