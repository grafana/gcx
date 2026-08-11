package adapter

import (
	"context"
	"encoding/json"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/grafana-app-sdk/logging"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// RegistryAccess is the subset of discovery.Registry needed for adapter registration.
type RegistryAccess interface {
	RegisterAdapter(factory Factory, desc resources.Descriptor, aliases []string)
}

// Registration holds a pre-resolved adapter factory with its descriptor and aliases.
// Populated lazily by calling the factory once to extract descriptor metadata.
type Registration struct {
	Factory     Factory
	Descriptor  resources.Descriptor
	Aliases     []string
	GVK         schema.GroupVersionKind
	Schema      json.RawMessage                // Required, non-nil JSON Schema for this resource type (per CONSTITUTION.md).
	Example     json.RawMessage                // Example manifest (YAML-compatible JSON, per CONSTITUTION.md). MAY be nil for read-only resources.
	Operations  map[string]agent.OperationHint // Agent metadata: per-operation token cost and hint, keyed by "get", "push", "pull", "delete".
	URLTemplate string                         // URL path template for deep links (e.g., "/a/grafana-slo-app/slo/{name}"). Empty means no deep link.
}

// registrations holds all adapter registrations collected from providers.
//
//nolint:gochecknoglobals // Self-registration pattern (same as providers.registry).
var registrations []Registration

// Register adds an adapter registration to the global registry.
// It is invoked by providers.Register() for each entry returned by
// Provider.TypedRegistrations() — provider code must not call it directly
// (CONSTITUTION.md § Architecture Invariants, unified provider registration).
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
