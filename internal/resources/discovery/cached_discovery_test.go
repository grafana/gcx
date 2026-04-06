package discovery_test

import (
	"strings"
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

func TestNewCachedRegistry(t *testing.T) {
	groups, resources := getSingleVersionDiscovery()
	client := &countingDiscoveryClient{
		groups:    groups,
		resources: resources,
	}

	// NewCachedRegistry is a test helper that delegates to NewRegistry.
	reg, err := discovery.NewCachedRegistry(t.Context(), client)
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, int32(1), client.calls.Load(), "should call discovery once")
}

func TestDiscoveryCacheDir_DifferentServers(t *testing.T) {
	dir1 := discovery.DiscoveryCacheDir("https://grafana-a.grafana.net", "")
	dir2 := discovery.DiscoveryCacheDir("https://grafana-b.grafana.net", "")

	assert.NotEqual(t, dir1, dir2, "different servers should have different cache dirs")
	assert.NotEmpty(t, dir1)
	assert.NotEmpty(t, dir2)
}

func TestDiscoveryCacheDir_StableForSameServer(t *testing.T) {
	dir1 := discovery.DiscoveryCacheDir("https://grafana.grafana.net", "")
	dir2 := discovery.DiscoveryCacheDir("https://grafana.grafana.net", "")

	assert.Equal(t, dir1, dir2, "same server should produce same cache dir")
}

func TestDiscoveryCacheDir_HashLength(t *testing.T) {
	// Hash should be 16 bytes = 32 hex chars for sufficient collision resistance.
	dir := discovery.DiscoveryCacheDir("https://grafana.grafana.net", "")
	parts := strings.Split(dir, "/")
	hash := parts[len(parts)-1]
	assert.Len(t, hash, 32, "hash should be 32 hex chars (16 bytes)")
}

func TestDiscoveryCacheDir_EnvOverridesDefault(t *testing.T) {
	customDir := t.TempDir()
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", customDir)

	// Env var should override even when no explicit overrideDir is passed.
	dir := discovery.DiscoveryCacheDir("https://grafana.grafana.net", "")
	assert.Equal(t, customDir, dir, "env var should override default")
}

func TestDiscoveryCacheDir_EnvOverridesExplicitDir(t *testing.T) {
	envDir := t.TempDir()
	explicitDir := t.TempDir()
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", envDir)

	// Env var takes precedence over explicit overrideDir.
	dir := discovery.DiscoveryCacheDir("https://grafana.grafana.net", explicitDir)
	assert.Equal(t, envDir, dir, "env var should override explicit dir")
}

func TestDiscoveryCacheDir_RelativeEnvIgnored(t *testing.T) {
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", "relative/path")

	// Relative path in env var should be ignored.
	dir := discovery.DiscoveryCacheDir("https://grafana.grafana.net", "")
	assert.NotEqual(t, "relative/path", dir, "relative env var should be ignored")
	assert.True(t, strings.HasPrefix(dir, "/"), "should fall through to absolute default path")
}

func TestDiscoveryCacheDir_ExplicitOverrideDir(t *testing.T) {
	explicitDir := t.TempDir()

	dir := discovery.DiscoveryCacheDir("https://grafana.grafana.net", explicitDir)
	assert.Equal(t, explicitDir, dir, "explicit dir should be used when no env var set")
}

func TestNewDefaultRegistryWithCacheDir(t *testing.T) {
	cfg := config.NamespacedRESTConfig{}
	cfg.Host = "https://test.grafana.net"

	// This will fail to connect (no server), but should not panic.
	_, err := discovery.NewDefaultRegistryWithCacheDir(t.Context(), cfg, t.TempDir())
	require.Error(t, err, "should fail because there's no server to connect to")
}
