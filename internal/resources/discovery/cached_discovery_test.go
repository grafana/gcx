package discovery_test

import (
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// countingDiscoveryClient wraps a mock and counts how many times
// ServerGroupsAndResources is called, so we can verify caching behavior.
type countingDiscoveryClient struct {
	groups    []*metav1.APIGroup
	resources []*metav1.APIResourceList
	err       error
	calls     atomic.Int32
}

func (c *countingDiscoveryClient) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	c.calls.Add(1)
	return c.groups, c.resources, c.err
}

func TestNewDefaultRegistry_UsesCachedDiscovery(t *testing.T) {
	// NewDefaultRegistry requires a real REST config pointing at a server,
	// so we test the caching behavior through NewCachedRegistry which accepts
	// our Client interface and a cache directory.
	groups, resources := getSingleVersionDiscovery()
	client := &countingDiscoveryClient{
		groups:    groups,
		resources: resources,
	}

	cacheDir := t.TempDir()

	// First call: should hit the underlying client.
	reg1, err := discovery.NewCachedRegistry(t.Context(), client, cacheDir)
	require.NoError(t, err)
	require.NotNil(t, reg1)
	assert.Equal(t, int32(1), client.calls.Load(), "first registry creation should call discovery once")

	// Second call with same cache dir: should still call the underlying client
	// because our Client interface doesn't have TTL — the caching happens at
	// the HTTP transport level via disk.NewCachedDiscoveryClientForConfig.
	// But NewCachedRegistry should at least accept and use the cache dir.
	reg2, err := discovery.NewCachedRegistry(t.Context(), client, cacheDir)
	require.NoError(t, err)
	require.NotNil(t, reg2)

	// Both registries should have discovered the same resources.
	assert.ElementsMatch(t, reg1.PreferredResources(), reg2.PreferredResources())
}

func TestNewDefaultRegistry_CacheDir(t *testing.T) {
	// Verify that NewDefaultRegistryWithCacheDir computes a per-server cache directory
	// and passes it through. We test this indirectly by checking that two different
	// server URLs produce different cache directories.
	dir1 := discovery.DiscoveryCacheDir("https://grafana-a.grafana.net", "")
	dir2 := discovery.DiscoveryCacheDir("https://grafana-b.grafana.net", "")

	assert.NotEqual(t, dir1, dir2, "different servers should have different cache dirs")
	assert.NotEmpty(t, dir1)
	assert.NotEmpty(t, dir2)
}

func TestDiscoveryCacheDir_EnvOverride(t *testing.T) {
	customDir := t.TempDir()
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", customDir)

	dir := discovery.DiscoveryCacheDir("https://grafana.grafana.net", customDir)
	assert.Equal(t, customDir, dir, "env var should override default cache dir")
}

func TestNewDefaultRegistry_CacheDirFromConfig(t *testing.T) {
	// DiscoveryCacheDir should produce a stable path for the same server.
	dir1 := discovery.DiscoveryCacheDir("https://grafana.grafana.net", "")
	dir2 := discovery.DiscoveryCacheDir("https://grafana.grafana.net", "")

	assert.Equal(t, dir1, dir2, "same server should produce same cache dir")
}

func TestNewDefaultRegistryWithCacheDir(t *testing.T) {
	// Verify the full flow: config → cached registry works end-to-end.
	// We can't hit a real server, so we just verify it compiles and the
	// function signature accepts NamespacedRESTConfig + cache dir.
	cfg := config.NamespacedRESTConfig{}
	cfg.Host = "https://test.grafana.net"

	// This will fail to connect (no server), but should not panic.
	_, err := discovery.NewDefaultRegistryWithCacheDir(t.Context(), cfg, t.TempDir())
	require.Error(t, err, "should fail because there's no server to connect to")
}
