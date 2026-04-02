package faro_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name:  "returns raw JSON",
			appID: "42",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugins/grafana-kowalski-app/resources/api/v1/app/42/sourcemaps", r.URL.Path)
				writeJSON(w, []map[string]any{
					{"id": "bundle-1", "version": "1.0.0"},
				})
			},
		},
		{
			name:  "returns error on server error",
			appID: "42",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			c := newTestClient(t, server)
			result, err := c.ListSourcemaps(t.Context(), tt.appID)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, result)
		})
	}
}

func TestClient_UploadSourcemap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/plugins/grafana-kowalski-app/resources/api/v1/app/42/sourcemaps", r.URL.Path)
		writeJSON(w, map[string]any{"status": "ok"})
	}))
	defer server.Close()

	c := newTestClient(t, server)
	result, err := c.UploadSourcemap(t.Context(), "42", strings.NewReader("sourcemap-data"))
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

func TestClient_DeleteSourcemap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/plugins/grafana-kowalski-app/resources/api/v1/app/42/sourcemaps/bundle-1", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := newTestClient(t, server)
	err := c.DeleteSourcemap(t.Context(), "42", "bundle-1")
	require.NoError(t, err)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
