package incidents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	internalconfig "github.com/grafana/grafanactl/internal/config"
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// staticDescriptor is the resource descriptor for incident resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "incident.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     "Incident",
	Singular: "incident",
	Plural:   "incidents",
}

// staticAliases are the short aliases for incident resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticAliases = []string{"incidents", "incident", "inc"}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	loader := &providers.ConfigLoader{}
	adapter.Register(adapter.Registration{
		Factory:    NewAdapterFactory(loader),
		Descriptor: staticDescriptor,
		Aliases:    staticAliases,
		GVK:        staticDescriptor.GroupVersionKind(),
		Schema:     incidentSchema(),
		Example:    incidentExample(),
	})
}

// incidentSchema returns a JSON Schema for the Incident resource type.
func incidentSchema() json.RawMessage {
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/Incident",
		"type":    "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": APIVersion},
			"kind":       map[string]any{"type": "string", "const": Kind},
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      map[string]any{"type": "string"},
					"namespace": map[string]any{"type": "string"},
				},
			},
			"spec": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":        map[string]any{"type": "string"},
					"status":       map[string]any{"type": "string"},
					"severity":     map[string]any{"type": "string"},
					"severityID":   map[string]any{"type": "string"},
					"isDrill":      map[string]any{"type": "boolean"},
					"incidentType": map[string]any{"type": "string"},
					"description":  map[string]any{"type": "string"},
					"labels":       map[string]any{"type": "array"},
				},
				"required": []string{"title", "status"},
			},
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}
	b, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("incidents: failed to marshal schema: %v", err))
	}
	return b
}

// incidentExample returns an example Incident manifest as JSON.
func incidentExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "my-incident",
		},
		"spec": map[string]any{
			"title":        "Service degradation in production",
			"status":       "active",
			"isDrill":      false,
			"incidentType": "internal",
			"labels": []map[string]any{
				{"key": "team", "label": "platform"},
				{"key": "env", "label": "production"},
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("incidents: failed to marshal example: %v", err))
	}
	return b
}

// GrafanaConfigLoader can load a NamespacedRESTConfig from the active context.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (internalconfig.NamespacedRESTConfig, error)
}

// NewAdapterFactory returns a lazy adapter.Factory for incidents.
// The factory captures the GrafanaConfigLoader and constructs the client on first invocation.
func NewAdapterFactory(loader GrafanaConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load REST config for incidents adapter: %w", err)
		}

		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create incidents client: %w", err)
		}

		return &ResourceAdapter{
			client:    client,
			namespace: cfg.Namespace,
		}, nil
	}
}

// NewFactoryFromConfig returns an adapter.Factory for incidents that
// creates a Client using the provided NamespacedRESTConfig.
func NewFactoryFromConfig(cfg internalconfig.NamespacedRESTConfig) adapter.Factory {
	return func(_ context.Context) (adapter.ResourceAdapter, error) {
		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create incidents client: %w", err)
		}

		return &ResourceAdapter{
			client:    client,
			namespace: cfg.Namespace,
		}, nil
	}
}

// ResourceAdapter bridges the incidents Client to the grafanactl resources pipeline.
type ResourceAdapter struct {
	client    *Client
	namespace string
}

var _ adapter.ResourceAdapter = &ResourceAdapter{}

// Descriptor returns the resource descriptor this adapter serves.
func (a *ResourceAdapter) Descriptor() resources.Descriptor {
	return staticDescriptor
}

// Aliases returns short names for selector resolution.
func (a *ResourceAdapter) Aliases() []string {
	return staticAliases
}

// List returns all incident resources as unstructured objects.
func (a *ResourceAdapter) List(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	incidents, err := a.client.List(ctx, IncidentQuery{})
	if err != nil {
		return nil, fmt.Errorf("failed to list incidents: %w", err)
	}

	result := &unstructured.UnstructuredList{}
	for _, inc := range incidents {
		res, err := ToResource(inc, a.namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to convert incident %q to resource: %w", inc.IncidentID, err)
		}
		result.Items = append(result.Items, res.Object)
	}

	return result, nil
}

// Get returns a single incident resource by ID.
func (a *ResourceAdapter) Get(ctx context.Context, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	inc, err := a.client.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get incident %q: %w", name, err)
	}

	res, err := ToResource(*inc, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to convert incident %q to resource: %w", name, err)
	}

	obj := res.ToUnstructured()
	return &obj, nil
}

// Create creates a new incident resource from an unstructured object.
func (a *ResourceAdapter) Create(ctx context.Context, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	res, err := resources.FromUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert unstructured to resource: %w", err)
	}

	inc, err := FromResource(res)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource to incident: %w", err)
	}

	created, err := a.client.Create(ctx, inc)
	if err != nil {
		return nil, fmt.Errorf("failed to create incident: %w", err)
	}

	createdRes, err := ToResource(*created, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to convert created incident to resource: %w", err)
	}

	createdObj := createdRes.ToUnstructured()
	return &createdObj, nil
}

// Update updates an existing incident's status from an unstructured object.
// The IRM API only supports status updates; other field changes are ignored.
func (a *ResourceAdapter) Update(ctx context.Context, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	res, err := resources.FromUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert unstructured to resource: %w", err)
	}

	inc, err := FromResource(res)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource to incident: %w", err)
	}

	id := obj.GetName()
	updated, err := a.client.UpdateStatus(ctx, id, inc.Status)
	if err != nil {
		return nil, fmt.Errorf("failed to update incident %q: %w", id, err)
	}

	updatedRes, err := ToResource(*updated, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to convert updated incident to resource: %w", err)
	}

	updatedObj := updatedRes.ToUnstructured()
	return &updatedObj, nil
}

// Delete is not supported for incidents. The IRM API does not expose a delete endpoint.
func (a *ResourceAdapter) Delete(_ context.Context, _ string, _ metav1.DeleteOptions) error {
	return errors.New("incidents: delete is not supported by the IRM API")
}
