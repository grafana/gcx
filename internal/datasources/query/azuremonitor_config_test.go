package query_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestGetAzureMonitorDefaultSubscription(t *testing.T) {
	tests := []struct {
		name     string
		jsonData map[string]any
		want     string
	}{
		{
			name:     "subscription from datasource jsonData",
			jsonData: map[string]any{"subscriptionId": "sub-default"},
			want:     "sub-default",
		},
		{
			name:     "missing subscriptionId returns empty",
			jsonData: map[string]any{},
			want:     "",
		},
		{
			name:     "non-string subscriptionId returns empty",
			jsonData: map[string]any{"subscriptionId": 42},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restCfg := azureMonitorDatasourceRESTConfig(t, tt.jsonData)

			subscription, err := dsquery.GetAzureMonitorDefaultSubscription(context.Background(), restCfg, "azmon-1")
			require.NoError(t, err)
			assert.Equal(t, tt.want, subscription)
		})
	}
}

func azureMonitorDatasourceRESTConfig(t *testing.T, jsonData map[string]any) config.NamespacedRESTConfig {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources/uid/azmon-1", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"uid":      "azmon-1",
			"name":     "azure",
			"type":     "grafana-azure-monitor-datasource",
			"jsonData": jsonData,
		}))
	}))
	t.Cleanup(srv.Close)

	return config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
}
