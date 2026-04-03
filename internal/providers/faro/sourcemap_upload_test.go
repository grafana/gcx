package faro_test

import (
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

func TestDiscoverFaroAPIURL(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantURL string
		wantErr bool
	}{
		{
			name: "extracts api_endpoint from plugin settings",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/plugins/grafana-kowalski-app/settings", r.URL.Path)
				writeJSON(w, map[string]any{
					"jsonData": map[string]any{
						"api_endpoint": "https://faro-api-dev.grafana.net/faro",
					},
				})
			},
			wantURL: "https://faro-api-dev.grafana.net/faro",
		},
		{
			name: "returns error when plugin not installed",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
		{
			name: "returns error when api_endpoint missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{
					"jsonData": map[string]any{},
				})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			cfg := config.NamespacedRESTConfig{
				Config:    rest.Config{Host: server.URL},
				Namespace: "stacks-13",
			}

			result, err := faro.DiscoverFaroAPIURL(t.Context(), cfg)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, result)
		})
	}
}

func TestGenerateBundleID(t *testing.T) {
	id := faro.GenerateBundleID()
	parts := strings.SplitN(id, "-", 2)
	require.Len(t, parts, 2, "bundle ID should be timestamp-hex")
	assert.Len(t, parts[1], 5, "hex suffix should be 5 chars")

	// Verify uniqueness
	id2 := faro.GenerateBundleID()
	assert.NotEqual(t, id, id2)
}

func TestUploadSourcemap(t *testing.T) {
	tests := []struct {
		name        string
		stackID     int
		token       string
		appID       string
		bundleID    string
		contentType string
		handler     http.HandlerFunc
		wantErr     bool
	}{
		{
			name:        "successful JSON upload",
			stackID:     13,
			token:       "my-token",
			appID:       "42",
			bundleID:    "1234567890-abc12",
			contentType: "application/json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/v1/app/42/sourcemaps/1234567890-abc12", r.URL.Path)
				assert.Equal(t, "Bearer 13:my-token", r.Header.Get("Authorization"))
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:        "successful gzip upload",
			stackID:     13,
			token:       "my-token",
			appID:       "42",
			bundleID:    "1234567890-abc12",
			contentType: "application/gzip",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "application/gzip", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:        "server error",
			stackID:     13,
			token:       "my-token",
			appID:       "42",
			bundleID:    "1234567890-abc12",
			contentType: "application/json",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("forbidden"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			err := faro.UploadSourcemap(
				t.Context(),
				server.URL,
				tt.stackID, tt.token,
				tt.appID, tt.bundleID,
				strings.NewReader("sourcemap-data"),
				tt.contentType,
			)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
