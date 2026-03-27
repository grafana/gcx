package config_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverStackID_Success(t *testing.T) {
	var pathCheck string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathCheck = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "stacks-12345",
			},
		})
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{Server: server.URL}

	stackID, err := config.DiscoverStackID(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "/bootdata", pathCheck)
	assert.Equal(t, int64(12345), stackID)
}

func TestDiscoverStackID_NonStackNamespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "grafana",
			},
		})
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{Server: server.URL}

	_, err := config.DiscoverStackID(t.Context(), cfg)
	assert.Error(t, err)
}

func TestDiscoverStackID_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{Server: server.URL}

	_, err := config.DiscoverStackID(t.Context(), cfg)
	assert.Error(t, err)
}

func TestDiscoverStackID_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{"))
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{Server: server.URL}

	_, err := config.DiscoverStackID(t.Context(), cfg)
	assert.Error(t, err)
}

func TestDiscoverStackID_TLSSkipVerify(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "stacks-678",
			},
		})
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{
		Server: server.URL,
		TLS: &config.TLS{
			Insecure: true,
		},
	}

	stackID, err := config.DiscoverStackID(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, int64(678), stackID)
}
