package probes

import (
	"context"
	"fmt"
	"strconv"

	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() { //nolint:gochecknoinits // Natural key registration for cross-stack push identity matching.
	adapter.RegisterNaturalKey(
		staticDescriptor.GroupVersionKind(),
		adapter.SpecFieldKey("name"),
	)
}

const (
	// APIVersion is the Kubernetes API version for SM Probe resources.
	APIVersion = "syntheticmonitoring.ext.grafana.app/v1alpha1"
	// Kind is the Kubernetes resource kind for SM probes.
	Kind = "Probe"
)

// staticDescriptor is the resource descriptor for SM Probe resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "syntheticmonitoring.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     "Probe",
	Singular: "probe",
	Plural:   "probes",
}

// StaticDescriptor returns the static descriptor for SM Probe resources.
// Used for registration without constructing an adapter instance.
func StaticDescriptor() resources.Descriptor {
	return staticDescriptor
}

// StaticGVK returns the static GroupVersionKind for SM Probe resources.
func StaticGVK() schema.GroupVersionKind {
	return staticDescriptor.GroupVersionKind()
}

// NewTypedCRUD creates a TypedCRUD for SM probes (read-only).
func NewTypedCRUD(ctx context.Context, loader smcfg.Loader) (*adapter.TypedCRUD[Probe], string, error) {
	baseURL, token, namespace, err := loader.LoadSMConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load SM config for probes: %w", err)
	}
	client := NewClient(ctx, baseURL, token)

	crud := &adapter.TypedCRUD[Probe]{
		ListFn: adapter.LimitedListFn(client.List),
		GetFn: func(ctx context.Context, name string) (*Probe, error) {
			id, err := strconv.ParseInt(name, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("probe name must be a numeric ID, got %q: %w", name, err)
			}
			return client.Get(ctx, id)
		},
		CreateFn: func(ctx context.Context, item *Probe) (*Probe, error) {
			item.Public = false
			resp, err := client.Create(ctx, *item)
			if err != nil {
				return nil, err
			}
			return &resp.Probe, nil
		},
		DeleteFn: func(ctx context.Context, name string) error {
			id, err := strconv.ParseInt(name, 10, 64)
			if err != nil {
				return fmt.Errorf("probe name must be a numeric ID, got %q: %w", name, err)
			}
			return client.Delete(ctx, id)
		},
		// UpdateFn nil — not yet supported
		Namespace:   namespace,
		StripFields: []string{"id", "tenantId", "created", "modified", "onlineChange", "online", "version"},
		Descriptor:  staticDescriptor,
	}
	return crud, namespace, nil
}

// NewAdapterFactory returns a lazy adapter.Factory for SM probes.
// The factory captures the smcfg.Loader and constructs the client on first invocation.
func NewAdapterFactory(loader smcfg.Loader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}
