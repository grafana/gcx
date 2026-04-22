package framework

import "github.com/grafana/gcx/internal/providers"

// DiscoverStatusDetectableFrom returns all providers in ps that implement StatusDetectable.
func DiscoverStatusDetectableFrom(ps []providers.Provider) []StatusDetectable {
	var out []StatusDetectable
	for _, p := range ps {
		if sd, ok := p.(StatusDetectable); ok {
			out = append(out, sd)
		}
	}
	return out
}

// DiscoverStatusDetectable returns all globally registered providers that implement StatusDetectable.
func DiscoverStatusDetectable() []StatusDetectable {
	return DiscoverStatusDetectableFrom(providers.All())
}

// DiscoverSetupableFrom returns all providers in ps that implement Setupable.
func DiscoverSetupableFrom(ps []providers.Provider) []Setupable {
	var out []Setupable
	for _, p := range ps {
		if s, ok := p.(Setupable); ok {
			out = append(out, s)
		}
	}
	return out
}

// DiscoverSetupable returns all globally registered providers that implement Setupable.
func DiscoverSetupable() []Setupable {
	return DiscoverSetupableFrom(providers.All())
}
