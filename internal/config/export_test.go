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

// StackBindingForTest builds the production credential binding for external
// integration tests without duplicating destination canonicalization rules.
func StackBindingForTest(path, name, server string, field credentials.Field) (credentials.Binding, error) {
	return StackBindingWithUserForTest(path, name, server, "", field)
}

// StackBindingWithUserForTest is StackBindingForTest with the basic-auth
// username included in password bindings.
func StackBindingWithUserForTest(path, name, server, user string, field credentials.Field) (credentials.Binding, error) {
	source, err := canonicalConfigSource(path)
	if err != nil {
		return credentials.Binding{}, err
	}
	stack := &StackConfig{
		sourceIdentity: source,
		Grafana:        &GrafanaConfig{Server: server, User: user},
	}
	if field == credentials.FieldSMToken {
		stack.Providers = map[string]map[string]string{"synth": {"sm-url": "https://sm.example.invalid"}}
	}
	return stackOwner(name, stack).binding(field), nil
}

// CloudBindingForTest builds the production Cloud credential binding for
// external integration tests.
func CloudBindingForTest(path, name string, field credentials.Field) (credentials.Binding, error) {
	source, err := canonicalConfigSource(path)
	if err != nil {
		return credentials.Binding{}, err
	}
	entry := &CloudEntry{sourceIdentity: source, APIUrl: "https://grafana.com", OAuthUrl: "https://grafana.com"}
	return cloudOwner(name, entry).binding(field), nil
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
