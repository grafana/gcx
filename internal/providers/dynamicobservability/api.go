package dynamicobservability

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/remote"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const apiGroup = "grafanadynamicobservabilityapp.ext.grafana.app"
const apiVersion = "v1alpha1"

type resourceClient interface {
	Get(ctx context.Context, desc resources.Descriptor, name string, opts metav1.GetOptions) (*unstructured.Unstructured, error)
	List(ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions) (*unstructured.UnstructuredList, error)
	Update(ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error)
}

type api struct {
	client resourceClient
	probes resources.Descriptor
	agents resources.Descriptor
}

func newAPI(ctx context.Context, loader *providers.ConfigLoader) (*api, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, err
	}
	client, registry, err := remote.NewDefaultClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	probes, err := resolve(registry, "dynamicprobes")
	if err != nil {
		return nil, err
	}
	agents, err := resolve(registry, "nodeagents")
	if err != nil {
		return nil, err
	}
	return &api{client: client, probes: probes, agents: agents}, nil
}

func resolve(registry *discovery.Registry, resource string) (resources.Descriptor, error) {
	filters, err := registry.MakeFilters(discovery.MakeFiltersOptions{Selectors: resources.Selectors{{
		Type:             resources.FilterTypeAll,
		GroupVersionKind: resources.PartialGVK{Group: apiGroup, Version: apiVersion, Resource: resource},
	}}})
	if err != nil {
		return resources.Descriptor{}, fmt.Errorf("dynamic observability %s API is unavailable: %w", resource, err)
	}
	if len(filters) != 1 {
		return resources.Descriptor{}, fmt.Errorf("expected one Dynamic Observability %s API, found %d", resource, len(filters))
	}
	return filters[0].Descriptor, nil
}

func decode[T any](obj *unstructured.Unstructured) (T, error) {
	var value T
	raw, err := json.Marshal(obj.Object)
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(raw, &value)
	return value, err
}

func list[T any](ctx context.Context, client resourceClient, desc resources.Descriptor) ([]T, error) {
	items, err := client.List(ctx, desc, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	result := make([]T, 0, len(items.Items))
	for i := range items.Items {
		item, err := decode[T](&items.Items[i])
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}
