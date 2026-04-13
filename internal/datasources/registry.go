package datasources

import "fmt"

// registry holds all datasource providers registered via RegisterProvider().
var registry []DatasourceProvider //nolint:gochecknoglobals // Self-registration pattern requires package-level state.

// RegisterProvider adds a datasource provider to the global registry.
// Panics if a provider with the same Kind() is already registered.
func RegisterProvider(dp DatasourceProvider) {
	for _, existing := range registry {
		if existing.Kind() == dp.Kind() {
			panic(fmt.Sprintf("DatasourceProvider kind %q already registered", dp.Kind()))
		}
	}
	registry = append(registry, dp)
}

// AllProviders returns all registered datasource providers.
// Returns a non-nil empty slice when no providers have been registered.
func AllProviders() []DatasourceProvider {
	if registry == nil {
		return []DatasourceProvider{}
	}
	return registry
}
