package providers

import "github.com/grafana/gcx/internal/resources/adapter"

// registry holds all providers registered via Register().
var registry []Provider //nolint:gochecknoglobals // Self-registration pattern requires package-level state.

// Register adds a provider to the global registry and auto-registers
// any adapter registrations from TypedRegistrations(). This makes
// provider identity and adapter registration atomic: a single
// providers.Register(p) call in init() populates both registries.
func Register(p Provider) {
	registry = append(registry, p)
	for _, reg := range p.TypedRegistrations() {
		adapter.Register(reg)
	}
}

// All returns all registered providers.
// Returns a non-nil empty slice when no providers have been registered.
func All() []Provider {
	if registry == nil {
		return []Provider{}
	}
	return registry
}
