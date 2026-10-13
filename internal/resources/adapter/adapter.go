// Package adapter defines the ResourceAdapter interface for bridging provider
// REST clients to the gcx resources pipeline.
package adapter

import (
	"context"
	"encoding/json"

	"github.com/grafana/gcx/internal/resources"
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

	// Schema returns the JSON Schema for this resource type. Adapters created
	// via TypedCRUD.AsAdapter() always derive this from T (SchemaFromType) —
	// it is never nil. Use SchemaForGVK() for the authoritative
	// global-registration lookup instead, when available.
	Schema() json.RawMessage

	// Example returns an example manifest for this resource type, or nil if
	// none was set (TypedCRUD.Example). Use ExampleForGVK() for the
	// authoritative global-registration lookup instead, when available.
	Example() json.RawMessage
}

// Factory is a lazy constructor for a ResourceAdapter.
// It is only invoked when a provider resource type is actually selected by a command,
// ensuring provider config is not loaded eagerly at startup.
type Factory func(ctx context.Context) (ResourceAdapter, error)

// ReadFailure identifies one unsuccessful read in a partially usable result.
// Resource may be nil when only the requested reference is known.
type ReadFailure struct {
	Resource *unstructured.Unstructured
	Err      error
}

// PartialReadError accompanies successful items with per-item coverage metadata.
// Ordinary collection failures must not implement this contract.
type PartialReadError interface {
	error
	ReadFailures() []ReadFailure
	SkippedReads() int
}

// MultipleGetter optionally preserves usable selections when another reference
// fails. Failures are returned through PartialReadError.
type MultipleGetter interface {
	GetMultiple(ctx context.Context, names []string, opts metav1.GetOptions) ([]unstructured.Unstructured, error)
}

// PullSelection retains every fetched object before identity-keyed insertion.
type PullSelection struct {
	Filter resources.Filter
	Items  []unstructured.Unstructured
}

// PullPreflight checks an object before processors or collection insertion.
type PullPreflight func(context.Context, resources.Filter, unstructured.Unstructured) error

// PullPreflighter optionally supplies a fresh identity check for one pull.
// The complete selection is supplied so duplicate identities remain observable.
type PullPreflighter interface {
	NewPullPreflight(selections []PullSelection) PullPreflight
}
