// Package k6 internal tests exercise unexported helpers (authenticatedClient, cache keys).
package k6

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

type mockLoader struct {
	cloudCfg    providers.CloudRESTConfig
	grafanaCfg  config.NamespacedRESTConfig
	providerCfg map[string]string
	saved       map[string]string
}

func (m *mockLoader) LoadCloudConfig(_ context.Context) (providers.CloudRESTConfig, error) {
	return m.cloudCfg, nil
}

func (m *mockLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return m.grafanaCfg, nil
}

func (m *mockLoader) LoadProviderConfig(_ context.Context, _ string) (map[string]string, string, error) {
	return m.providerCfg, "", nil
}

func (m *mockLoader) SaveProviderConfig(_ context.Context, _, key, value string) error {
	if m.saved == nil {
		m.saved = make(map[string]string)
	}
	m.saved[key] = value
	return nil
}

func TestAuthenticatedClient_SATokenColdPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/account/grafana-app/start" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"organization_id": "42", "v3_grafana_token": "fresh-tok",
			})
			return
		}
		t.Fatalf("unexpected: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	loader := &mockLoader{
		cloudCfg: providers.CloudRESTConfig{
			Stack:     cloud.StackInfo{ID: 999},
			Namespace: "stack-999",
		},
		grafanaCfg: config.NamespacedRESTConfig{
			Config: rest.Config{BearerToken: "glsa_test"},
		},
		providerCfg: map[string]string{"api-domain": srv.URL},
	}

	client, ns, err := authenticatedClient(context.Background(), loader)
	require.NoError(t, err)
	assert.Equal(t, "stack-999", ns)
	tok, _ := client.Token(context.Background())
	assert.Equal(t, "fresh-tok", tok)
	assert.Equal(t, "fresh-tok", loader.saved[keyCachedToken])
	assert.Equal(t, "999", loader.saved[keyCachedStackID])
}

func TestAuthenticatedClient_SATokenCachePath(t *testing.T) {
	loader := &mockLoader{
		cloudCfg: providers.CloudRESTConfig{
			Stack:     cloud.StackInfo{ID: 999},
			Namespace: "stack-999",
		},
		grafanaCfg: config.NamespacedRESTConfig{
			Config: rest.Config{BearerToken: "glsa_test"},
		},
		providerCfg: map[string]string{
			"api-domain":     "http://unused-because-cache-hit",
			keyCachedToken:   "cached-v3",
			keyCachedOrgID:   "42",
			keyCachedStackID: "999",
		},
	}

	client, _, err := authenticatedClient(context.Background(), loader)
	require.NoError(t, err)
	tok, _ := client.Token(context.Background())
	assert.Equal(t, "cached-v3", tok)
	// No exchange should have happened — SaveProviderConfig must not have been called.
	assert.Empty(t, loader.saved)
}

func TestAuthenticatedClient_SATokenMissingBearer(t *testing.T) {
	loader := &mockLoader{
		cloudCfg:   providers.CloudRESTConfig{Stack: cloud.StackInfo{ID: 999}},
		grafanaCfg: config.NamespacedRESTConfig{}, // empty BearerToken
	}
	_, _, err := authenticatedClient(context.Background(), loader)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "grafana.token is required")
}
