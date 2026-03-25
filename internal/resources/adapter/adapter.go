// Package adapter defines the ResourceAdapter interface for bridging provider
// REST clients to the grafanactl resources pipeline.
package adapter

import (
	"context"
	"encoding/json"

	"github.com/grafana/grafanactl/internal/resources"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ResourceAdapter bridges a provider's REST client to the resources pipeline.
// Each provider resource type (SLO definitions, Synth checks, etc.) implements
// this interface by wrapping its existing REST client and using its existing
// ToResource/FromResource adapter functions.
type ResourceAdapter interface {
	// List returns all resources of this type.
	List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error)

	// Get returns a single resource by name.
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*unstructured.Unstructured, error)

	// Create creates a new resource.
	Create(ctx context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions) (*unstructured.Unstructured, error)

	// Update updates an existing resource.
	Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error)

	// Delete removes a resource by name.
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error

	// Descriptor returns the resource descriptor this adapter serves.
	Descriptor() resources.Descriptor

	// Aliases returns short names for selector resolution (e.g., "slo", "checks").
	Aliases() []string

	// Schema returns the JSON Schema for this resource type, or nil if none is registered.
	Schema() json.RawMessage

	// Example returns an example manifest for this resource type, or nil if none is registered.
	Example() json.RawMessage
}

// Factory is a lazy constructor for a ResourceAdapter.
// It is only invoked when a provider resource type is actually selected by a command,
// ensuring provider config is not loaded eagerly at startup.
type Factory func(ctx context.Context) (ResourceAdapter, error)
