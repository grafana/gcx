package definitions

import (
	"context"
	"fmt"

	internalconfig "github.com/grafana/grafanactl/internal/config"
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// StaticDescriptor returns the resource descriptor for SLO definitions.
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "slo.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     "SLO",
		Singular: "slo",
		Plural:   "slos",
	}
}

// StaticAliases returns the short aliases for SLO resources.
func StaticAliases() []string {
	return []string{"slo"}
}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	desc := StaticDescriptor()
	adapter.Register(adapter.Registration{
		Factory:    NewLazyFactory(),
		Descriptor: desc,
		Aliases:    StaticAliases(),
		GVK:        desc.GroupVersionKind(),
	})
}

// NewLazyFactory returns an adapter.Factory that loads its config lazily from the
// default config file when invoked. This is used for global adapter registration in init()
// and by SLOProvider.ResourceAdapters().
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		var loader providers.ConfigLoader
		loader.SetContextName(internalconfig.ContextNameFromCtx(ctx))

		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load REST config for SLO adapter: %w", err)
		}

		return NewFactoryFromConfig(cfg)(ctx)
	}
}

// ResourceAdapter bridges the SLO definitions.Client to the grafanactl resources pipeline.
type ResourceAdapter struct {
	client    *Client
	namespace string
}

var _ adapter.ResourceAdapter = &ResourceAdapter{}

// NewFactoryFromConfig returns an adapter.Factory for SLO definitions that
// creates a definitions.Client using the provided NamespacedRESTConfig.
// The factory is lazy — the client is only created when the factory is invoked.
func NewFactoryFromConfig(cfg internalconfig.NamespacedRESTConfig) adapter.Factory {
	return func(_ context.Context) (adapter.ResourceAdapter, error) {
		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create SLO definitions client: %w", err)
		}

		return &ResourceAdapter{
			client:    client,
			namespace: cfg.Namespace,
		}, nil
	}
}

// Descriptor returns the resource descriptor this adapter serves.
func (a *ResourceAdapter) Descriptor() resources.Descriptor {
	return StaticDescriptor()
}

// Aliases returns short names for selector resolution.
func (a *ResourceAdapter) Aliases() []string {
	return StaticAliases()
}

// List returns all SLO resources as unstructured objects.
func (a *ResourceAdapter) List(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	slos, err := a.client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list SLOs: %w", err)
	}

	result := &unstructured.UnstructuredList{}
	for _, slo := range slos {
		res, err := ToResource(slo, a.namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to convert SLO %q to resource: %w", slo.UUID, err)
		}

		result.Items = append(result.Items, res.Object)
	}

	return result, nil
}

// Get returns a single SLO resource by name (UUID).
func (a *ResourceAdapter) Get(ctx context.Context, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	slo, err := a.client.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get SLO %q: %w", name, err)
	}

	res, err := ToResource(*slo, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to convert SLO %q to resource: %w", name, err)
	}

	obj := res.ToUnstructured()
	return &obj, nil
}

// Create creates a new SLO resource from an unstructured object.
func (a *ResourceAdapter) Create(ctx context.Context, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	res, err := resources.FromUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert unstructured to resource: %w", err)
	}

	slo, err := FromResource(res)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource to SLO: %w", err)
	}

	createResp, err := a.client.Create(ctx, slo)
	if err != nil {
		return nil, fmt.Errorf("failed to create SLO: %w", err)
	}

	// Fetch the created SLO to return its full representation.
	created, err := a.client.Get(ctx, createResp.UUID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch created SLO %q: %w", createResp.UUID, err)
	}

	createdRes, err := ToResource(*created, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to convert created SLO to resource: %w", err)
	}

	createdObj := createdRes.ToUnstructured()
	return &createdObj, nil
}

// Update updates an existing SLO resource from an unstructured object.
func (a *ResourceAdapter) Update(ctx context.Context, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	res, err := resources.FromUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert unstructured to resource: %w", err)
	}

	slo, err := FromResource(res)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource to SLO: %w", err)
	}

	uuid := obj.GetName()
	if err := a.client.Update(ctx, uuid, slo); err != nil {
		return nil, fmt.Errorf("failed to update SLO %q: %w", uuid, err)
	}

	// Fetch the updated SLO to return its full representation.
	updated, err := a.client.Get(ctx, uuid)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated SLO %q: %w", uuid, err)
	}

	updatedRes, err := ToResource(*updated, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to convert updated SLO to resource: %w", err)
	}

	updatedObj := updatedRes.ToUnstructured()
	return &updatedObj, nil
}

// Delete removes an SLO resource by name (UUID).
func (a *ResourceAdapter) Delete(ctx context.Context, name string, _ metav1.DeleteOptions) error {
	if err := a.client.Delete(ctx, name); err != nil {
		return fmt.Errorf("failed to delete SLO %q: %w", name, err)
	}

	return nil
}
