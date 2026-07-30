package azuremonitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestResolveSubscription(t *testing.T) {
	t.Run("flag value wins without fetching the datasource", func(t *testing.T) {
		cfg := datasourceRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected datasource fetch: %s", r.URL.Path)
		})

		subscription, err := resolveSubscription(context.Background(), cfg, "azmon-1", "sub-flag")
		require.NoError(t, err)
		assert.Equal(t, "sub-flag", subscription)
	})

	t.Run("falls back to the datasource default subscription", func(t *testing.T) {
		cfg := datasourceRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/datasources/uid/azmon-1", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"uid":      "azmon-1",
				"type":     "grafana-azure-monitor-datasource",
				"jsonData": map[string]any{"subscriptionId": "sub-default"},
			}))
		})

		subscription, err := resolveSubscription(context.Background(), cfg, "azmon-1", "")
		require.NoError(t, err)
		assert.Equal(t, "sub-default", subscription)
	})

	t.Run("errors when neither flag nor default is set", func(t *testing.T) {
		cfg := datasourceRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"uid":      "azmon-1",
				"type":     "grafana-azure-monitor-datasource",
				"jsonData": map[string]any{},
			}))
		})

		_, err := resolveSubscription(context.Background(), cfg, "azmon-1", "")
		require.Error(t, err)
		assert.ErrorContains(t, err, "--subscription is required")
	})
}

func datasourceRESTConfig(t *testing.T, handler http.HandlerFunc) config.NamespacedRESTConfig {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
}
