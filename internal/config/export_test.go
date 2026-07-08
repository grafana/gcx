package config

import (
	"strings"

	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/credentials"
)

// SetKeychainStoreFnForTest swaps the package-level keychainStoreFn and
// returns a function that restores the original value. Exposed solely for
// tests in config_test.
func SetKeychainStoreFnForTest(fn func() credentials.Store) func() {
	original := keychainStoreFn
	keychainStoreFn = fn
	return func() { keychainStoreFn = original }
}

// OnRefreshForTest returns the OnRefresh callback wired by WireTokenPersistence.
// Exposed solely for tests in config_test.
func (n *NamespacedRESTConfig) OnRefreshForTest() auth.TokenRefresher {
	if n.oauthTransport == nil {
		return nil
	}
	return n.oauthTransport.OnRefresh
}

// SeedStackIDCacheForTest primes the process-lifetime stack-ID discovery cache
// for a server, then returns a function that clears the whole cache. Exposed
// solely for tests in config_test that exercise the cache-peek mismatch path.
func SeedStackIDCacheForTest(server string, stackID int64) func() {
	stackIDCacheMu.Lock()
	stackIDCache[strings.TrimSuffix(server, "/")] = stackID
	stackIDCacheMu.Unlock()
	return func() {
		stackIDCacheMu.Lock()
		stackIDCache = map[string]int64{}
		stackIDCacheMu.Unlock()
	}
}
