package k6

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// staticDescriptor is the resource descriptor for k6 project resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "k6.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     "Project",
	Singular: "project",
	Plural:   "projects",
}

// staticAliases are the short aliases for k6 project resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticAliases = []string{"k6projects", "k6project", "k6proj"}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	loader := &providers.ConfigLoader{}
	adapter.Register(adapter.Registration{
		Factory:    NewAdapterFactory(loader),
		Descriptor: staticDescriptor,
		Aliases:    staticAliases,
		GVK:        staticDescriptor.GroupVersionKind(),
		Schema:     projectSchema(),
		Example:    projectExample(),
	})
}

// projectSchema returns a JSON Schema for the Project resource type.
func projectSchema() json.RawMessage {
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/k6/Project",
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
					"name":               map[string]any{"type": "string"},
					"is_default":         map[string]any{"type": "boolean"},
					"grafana_folder_uid": map[string]any{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}
	b, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("k6: failed to marshal schema: %v", err))
	}
	return b
}

// projectExample returns an example Project manifest as JSON.
func projectExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "12345",
		},
		"spec": map[string]any{
			"name":       "my-project",
			"is_default": false,
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("k6: failed to marshal example: %v", err))
	}
	return b
}

// CloudConfigLoader can load Grafana Cloud config (token + stack info via GCOM).
type CloudConfigLoader interface {
	LoadCloudConfig(ctx context.Context) (providers.CloudRESTConfig, error)
}

// NewAdapterFactory returns a lazy adapter.Factory for k6 projects.
func NewAdapterFactory(loader CloudConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		client, ns, err := authenticatedClient(ctx, loader, "")
		if err != nil {
			return nil, err
		}
		return &ResourceAdapter{
			client:    client,
			namespace: ns,
		}, nil
	}
}

// authenticatedClient loads cloud config, performs k6 token exchange, and returns an authenticated client.
// apiDomainOverride can be empty to use default.
func authenticatedClient(ctx context.Context, loader CloudConfigLoader, apiDomainOverride string) (*Client, string, error) {
	cfg, err := loader.LoadCloudConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("k6: load cloud config: %w", err)
	}

	domain := apiDomainOverride
	if domain == "" {
		domain = DefaultAPIDomain
	}

	client := NewClient(domain)
	if err := client.Authenticate(ctx, cfg.Token, cfg.Stack.ID); err != nil {
		return nil, "", fmt.Errorf("k6 auth failed (PUT %s): %w -- ensure your token has k6 scopes", authPath, err)
	}

	return client, cfg.Namespace, nil
}

// ResourceAdapter bridges the k6 Client to the grafanactl resources pipeline.
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

// List returns all project resources as unstructured objects.
func (a *ResourceAdapter) List(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	projects, err := a.client.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list k6 projects: %w", err)
	}

	result := &unstructured.UnstructuredList{}
	for _, p := range projects {
		res, err := ToResource(p, a.namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to convert project %d to resource: %w", p.ID, err)
		}
		result.Items = append(result.Items, res.Object)
	}
	return result, nil
}

// Get returns a single project resource by ID.
func (a *ResourceAdapter) Get(ctx context.Context, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	id, err := strconv.Atoi(name)
	if err != nil {
		return nil, fmt.Errorf("k6: invalid project ID %q: %w", name, err)
	}

	p, err := a.client.GetProject(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get k6 project %q: %w", name, err)
	}

	res, err := ToResource(*p, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to convert project %q to resource: %w", name, err)
	}

	obj := res.ToUnstructured()
	return &obj, nil
}

// Create creates a new project resource from an unstructured object.
func (a *ResourceAdapter) Create(ctx context.Context, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	res, err := resources.FromUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert unstructured to resource: %w", err)
	}

	p, err := FromResource(res)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource to project: %w", err)
	}

	created, err := a.client.CreateProject(ctx, p.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to create k6 project: %w", err)
	}

	createdRes, err := ToResource(*created, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to convert created project to resource: %w", err)
	}

	createdObj := createdRes.ToUnstructured()
	return &createdObj, nil
}

// Update updates an existing project's name from an unstructured object.
func (a *ResourceAdapter) Update(ctx context.Context, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	res, err := resources.FromUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert unstructured to resource: %w", err)
	}

	p, err := FromResource(res)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource to project: %w", err)
	}

	name := obj.GetName()
	id, err := strconv.Atoi(name)
	if err != nil {
		return nil, fmt.Errorf("k6: invalid project ID %q: %w", name, err)
	}

	if err := a.client.UpdateProject(ctx, id, p.Name); err != nil {
		return nil, fmt.Errorf("failed to update k6 project %d: %w", id, err)
	}

	// Re-fetch to get updated state.
	updated, err := a.client.GetProject(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated project %d: %w", id, err)
	}

	updatedRes, err := ToResource(*updated, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to convert updated project to resource: %w", err)
	}

	updatedObj := updatedRes.ToUnstructured()
	return &updatedObj, nil
}

// Delete deletes a project by ID.
func (a *ResourceAdapter) Delete(ctx context.Context, name string, _ metav1.DeleteOptions) error {
	id, err := strconv.Atoi(name)
	if err != nil {
		return fmt.Errorf("k6: invalid project ID %q: %w", name, err)
	}
	return a.client.DeleteProject(ctx, id)
}
