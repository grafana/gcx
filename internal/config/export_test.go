package config

import "github.com/grafana/gcx/internal/auth"

// OnRefreshForTest returns the OnRefresh callback wired by WireTokenPersistence.
// Exposed solely for tests in config_test.
func (n *NamespacedRESTConfig) OnRefreshForTest() auth.TokenRefresher {
	if n.oauthTransport == nil {
		return nil
	}
	return n.oauthTransport.OnRefresh
}
