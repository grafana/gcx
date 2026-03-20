package adapter

import (
	"context"
	"encoding/json"

	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/grafana/grafanactl/internal/resources"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// DescriptorProvider describes a type that can supply adapter factories.
// This matches the shape of providers.Provider but avoids importing the providers package.
type DescriptorProvider interface {
	ResourceAdapters() []Factory
}

// RegistryAccess is the subset of discovery.Registry needed for adapter registration.
type RegistryAccess interface {
	RegisterAdapter(factory Factory, desc resources.Descriptor, aliases []string)
}

// Registration holds a pre-resolved adapter factory with its descriptor and aliases.
// Populated lazily by calling the factory once to extract descriptor metadata.
type Registration struct {
	Factory    Factory
	Descriptor resources.Descriptor
	Aliases    []string
	GVK        schema.GroupVersionKind
	Schema     json.RawMessage // Optional JSON Schema for this resource type
	Example    json.RawMessage // Optional example manifest (YAML-compatible JSON)
}

// registrations holds all adapter registrations collected from providers.
//
//nolint:gochecknoglobals // Self-registration pattern (same as providers.registry).
var registrations []Registration

// Register adds an adapter registration to the global registry.
// Providers call this from their init() function alongside providers.Register().
func Register(reg Registration) {
	registrations = append(registrations, reg)
}

// AllRegistrations returns all registered adapter registrations.
func AllRegistrations() []Registration {
	return registrations
}

// RegisterAll registers all globally-registered adapter factories into the
// given discovery registry. This should be called after creating a Registry
// in resource command setup.
func RegisterAll(ctx context.Context, reg RegistryAccess) {
	logger := logging.FromContext(ctx)
	for _, r := range registrations {
		logger.Debug("registering provider adapter",
			"gvk", r.GVK.String(),
			"aliases", r.Aliases,
		)
		reg.RegisterAdapter(r.Factory, r.Descriptor, r.Aliases)
	}
}

// SchemaForGVK returns the registered schema for the given GVK, or nil.
func SchemaForGVK(gvk schema.GroupVersionKind) json.RawMessage {
	for _, r := range registrations {
		if r.GVK == gvk && r.Schema != nil {
			return r.Schema
		}
	}
	return nil
}

// ExampleForGVK returns the registered example for the given GVK, or nil.
func ExampleForGVK(gvk schema.GroupVersionKind) json.RawMessage {
	for _, r := range registrations {
		if r.GVK == gvk && r.Example != nil {
			return r.Example
		}
	}
	return nil
}
