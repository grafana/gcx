// Package k6 internal tests exercise unexported helpers (authenticatedClient, cache keys).
package k6

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

type mockLoader struct {
	cloudCfg     providers.CloudRESTConfig
	grafanaCfg   config.NamespacedRESTConfig
	providerCfg  map[string]string
	envEndpoints map[string]bool
	saved        map[string]string
}

func (m *mockLoader) LoadDirectProviderSnapshot(_ context.Context, _ providers.DirectProviderPolicy) (providers.DirectProviderSnapshot, error) {
	return providers.DirectProviderSnapshot{
		ProviderConfig:           m.providerCfg,
		Namespace:                m.cloudCfg.Namespace,
		GrafanaConfig:            &m.grafanaCfg,
		RuntimeEndpointOverrides: m.envEndpoints,
		ResolveCloudConfig: func(context.Context) (providers.CloudRESTConfig, error) {
			return m.cloudCfg, nil
		},
	}, nil
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
	assert.Equal(t, srv.URL, loader.saved[keyCachedDomain])
	assert.Equal(t, cacheBinding("fresh-tok", 42, 999, srv.URL), loader.saved[keyCachedBinding])
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
			keyCachedDomain:  "http://unused-because-cache-hit",
			keyCachedBinding: cacheBinding("cached-v3", 42, 999, "http://unused-because-cache-hit"),
		},
	}

	client, _, err := authenticatedClient(context.Background(), loader)
	require.NoError(t, err)
	tok, _ := client.Token(context.Background())
	assert.Equal(t, "cached-v3", tok)
	// No exchange should have happened — SaveProviderConfig must not have been called.
	assert.Empty(t, loader.saved)
}

func TestAuthenticatedClient_RuntimeDomainOverrideBypassesStoredCache(t *testing.T) {
	var startRequests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/account/grafana-app/start" {
			t.Fatalf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		startRequests.Add(1)
		assert.Equal(t, "glsa_runtime", r.Header.Get("X-Grafana-Service-Token"))
		assert.Equal(t, "999", r.Header.Get("X-Stack-Id"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"organization_id": "77", "v3_grafana_token": "runtime-v3",
		})
	}))
	t.Cleanup(srv.Close)

	loader := &mockLoader{
		cloudCfg: providers.CloudRESTConfig{
			Stack:     cloud.StackInfo{ID: 999},
			Namespace: "stack-999",
		},
		grafanaCfg: config.NamespacedRESTConfig{
			Config: rest.Config{BearerToken: "glsa_runtime"},
		},
		providerCfg: map[string]string{
			"api-domain":     srv.URL,
			keyCachedToken:   "stored-v3-must-not-be-used",
			keyCachedOrgID:   "42",
			keyCachedStackID: "999",
			keyCachedDomain:  srv.URL,
			keyCachedBinding: cacheBinding("stored-v3-must-not-be-used", 42, 999, srv.URL),
		},
		envEndpoints: map[string]bool{"api-domain": true},
	}

	client, _, err := authenticatedClient(context.Background(), loader)
	require.NoError(t, err)
	assert.Equal(t, int32(1), startRequests.Load(), "runtime destinations require a fresh exchange")
	tok, _ := client.Token(context.Background())
	assert.Equal(t, "runtime-v3", tok)
	assert.Equal(t, "runtime-v3", loader.saved[keyCachedToken])
	assert.Equal(t, srv.URL, loader.saved[keyCachedDomain])
	assert.Equal(t, cacheBinding("runtime-v3", 77, 999, srv.URL), loader.saved[keyCachedBinding])
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

// TestAuthenticatedClient_SATokenReauthOn401Chain verifies the end-to-end
// integration of the reauth callback wired in authenticatedClient:
// stale cache hit -> 401 from API call -> clearCache -> fresh exchange ->
// persistCache -> retry succeeds with new token.
func TestAuthenticatedClient_SATokenReauthOn401Chain(t *testing.T) {
	var apiCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/account/grafana-app/start" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"organization_id": "55", "v3_grafana_token": "fresh-after-401",
			})
			return
		}
		if r.URL.Path == "/cloud/v6/projects" {
			call := apiCalls.Add(1)
			if call == 1 {
				// First call uses the stale cached token: server rejects.
				assert.Equal(t, "Bearer stale-cached", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Retry uses the freshly-exchanged token.
			assert.Equal(t, "Bearer fresh-after-401", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"value": []any{}})
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
		providerCfg: map[string]string{
			"api-domain":     srv.URL,
			keyCachedToken:   "stale-cached",
			keyCachedOrgID:   "42",
			keyCachedStackID: "999",
			keyCachedDomain:  srv.URL,
			keyCachedBinding: cacheBinding("stale-cached", 42, 999, srv.URL),
		},
	}

	client, _, err := authenticatedClient(context.Background(), loader)
	require.NoError(t, err)

	// Issue an API call; the stale token triggers 401, reauth runs the chain.
	require.NoError(t, callListProjects(t, client))

	// Two API calls happened (401 then retry).
	assert.Equal(t, int32(2), apiCalls.Load())

	// loader.saved must reflect a fresh persistCache call with the new credentials.
	assert.Equal(t, "fresh-after-401", loader.saved[keyCachedToken])
	assert.Equal(t, "55", loader.saved[keyCachedOrgID])
	assert.Equal(t, "999", loader.saved[keyCachedStackID])
	assert.Equal(t, srv.URL, loader.saved[keyCachedDomain])
	assert.Equal(t, cacheBinding("fresh-after-401", 55, 999, srv.URL), loader.saved[keyCachedBinding])
}

// callListProjects is a tiny helper so the integration test doesn't need to
// know about the API interface methods beyond ListProjects.
func callListProjects(t *testing.T, client API) error {
	t.Helper()
	_, err := client.ListProjects(context.Background())
	return err
}
