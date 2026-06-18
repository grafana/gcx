package query_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestGetPyroscopeConfig(t *testing.T) {
	tests := []struct {
		name     string
		jsonData map[string]any
		want     time.Duration
	}{
		{
			name:     "minStep from datasource jsonData",
			jsonData: map[string]any{"minStep": "1m"},
			want:     time.Minute,
		},
		{
			name:     "extended minStep unit",
			jsonData: map[string]any{"minStep": "7d"},
			want:     7 * 24 * time.Hour,
		},
		{
			name:     "missing minStep defaults to 15s",
			jsonData: map[string]any{},
			want:     15 * time.Second,
		},
		{
			name:     "invalid minStep defaults to 15s",
			jsonData: map[string]any{"minStep": "not-a-duration"},
			want:     15 * time.Second,
		},
		{
			name:     "zero minStep defaults to 15s",
			jsonData: map[string]any{"minStep": "0s"},
			want:     15 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restCfg := pyroscopeDatasourceRESTConfig(t, tt.jsonData)

			cfg, err := dsquery.GetPyroscopeConfig(context.Background(), restCfg, "pyro-1")
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.MinStep)
		})
	}
}

func pyroscopeDatasourceRESTConfig(t *testing.T, jsonData map[string]any) config.NamespacedRESTConfig {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources/uid/pyro-1", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"uid":      "pyro-1",
			"name":     "profiles",
			"type":     "grafana-pyroscope-datasource",
			"jsonData": jsonData,
		}))
	}))
	t.Cleanup(srv.Close)

	return config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
}
