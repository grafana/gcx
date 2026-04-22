package annotations

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// APIVersion is the API version for Annotation resources.
	APIVersion = "annotations.ext.grafana.app/v1alpha1"
	// Kind is the kind for Annotation resources.
	Kind = "Annotation"
)

// staticDescriptor is the resource descriptor for Annotation resources.
//
//nolint:gochecknoglobals // Static descriptor used in self-registration pattern.
var staticDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "annotations.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     Kind,
	Singular: "annotation",
	Plural:   "annotations",
}

// StaticDescriptor returns the descriptor for Annotation resources.
func StaticDescriptor() resources.Descriptor {
	return staticDescriptor
}

// AnnotationSchema returns a JSON Schema for the Annotation resource type,
// derived from the Annotation struct via reflection.
func AnnotationSchema() json.RawMessage {
	return adapter.SchemaFromType[Annotation](staticDescriptor)
}

// AnnotationExample returns an example Annotation manifest as JSON.
func AnnotationExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "42",
		},
		"spec": map[string]any{
			"dashboardUID": "abcdef123",
			"text":         "Deploy v2.3.0",
			"tags":         []string{"deploy", "prod"},
			"time":         1700000000000,
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("annotations: failed to marshal example: %v", err))
	}
	return b
}

// RESTConfigLoader can load a NamespacedRESTConfig from the active context.
type RESTConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (internalconfig.NamespacedRESTConfig, error)
}

// NewAdapterFactory returns a lazy adapter.Factory for Annotation resources.
// The factory captures the RESTConfigLoader and constructs the client on first
// invocation.
func NewAdapterFactory(loader RESTConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load REST config for annotations adapter: %w", err)
		}

		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create annotations client: %w", err)
		}

		return newTypedAdapter(client, cfg.Namespace), nil
	}
}

// NewFactoryFromConfig returns an adapter.Factory for Annotation resources that
// creates a Client using the provided NamespacedRESTConfig.
func NewFactoryFromConfig(cfg internalconfig.NamespacedRESTConfig) adapter.Factory {
	return func(_ context.Context) (adapter.ResourceAdapter, error) {
		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create annotations client: %w", err)
		}
		return newTypedAdapter(client, cfg.Namespace), nil
	}
}

// newTypedAdapter builds the TypedCRUD[Annotation] adapter for the given
// client and namespace. It uses the default (unfiltered) List semantics — CLI
// commands that need --from/--to/--tags plumbing build their own TypedCRUD
// with a scoped ListFn closure.
func newTypedAdapter(client *Client, namespace string) adapter.ResourceAdapter {
	crud := buildTypedCRUD(client, namespace, ListOptions{})
	return crud.AsAdapter()
}

// buildTypedCRUD assembles a TypedCRUD[Annotation] around the given client,
// namespace, and base list options. The list options let the CLI pass through
// --from/--to/--tags/--limit without mutating the client.
func buildTypedCRUD(client *Client, namespace string, baseListOpts ListOptions) *adapter.TypedCRUD[Annotation] {
	return &adapter.TypedCRUD[Annotation]{
		ListFn: func(ctx context.Context, limit int64) ([]Annotation, error) {
			opts := baseListOpts
			if limit > 0 {
				opts.Limit = int(limit)
			}
			return client.List(ctx, opts)
		},

		GetFn: func(ctx context.Context, name string) (*Annotation, error) {
			id, err := strconv.ParseInt(name, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid annotation ID %q: %w", name, err)
			}
			return client.Get(ctx, id)
		},

		CreateFn: func(ctx context.Context, a *Annotation) (*Annotation, error) {
			if err := client.Create(ctx, a); err != nil {
				return nil, err
			}
			return a, nil
		},

		UpdateFn: func(ctx context.Context, name string, a *Annotation) (*Annotation, error) {
			id, err := strconv.ParseInt(name, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid annotation ID %q: %w", name, err)
			}
			// PATCH the annotation as a full object: marshal to JSON, then
			// unmarshal into a patch map so the existing Client.Update signature
			// (which takes map[string]any) remains unchanged.
			patch, perr := annotationToPatch(a)
			if perr != nil {
				return nil, perr
			}
			if err := client.Update(ctx, id, patch); err != nil {
				return nil, err
			}
			// The Grafana API does not echo back the updated object, so return
			// the caller's annotation with the ID set from the path.
			a.ID = id
			return a, nil
		},

		DeleteFn: func(ctx context.Context, name string) error {
			id, err := strconv.ParseInt(name, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid annotation ID %q: %w", name, err)
			}
			return client.Delete(ctx, id)
		},

		StripFields: []string{"id"},
		Namespace:   namespace,
		Descriptor:  staticDescriptor,
	}
}

// annotationToPatch marshals an Annotation to a patch map suitable for PATCH,
// omitting the server-assigned ID.
func annotationToPatch(a *Annotation) (map[string]any, error) {
	data, err := json.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal annotation: %w", err)
	}
	var patch map[string]any
	if err := json.Unmarshal(data, &patch); err != nil {
		return nil, fmt.Errorf("failed to build annotation patch: %w", err)
	}
	delete(patch, "id")
	return patch, nil
}

// NewTypedCRUD creates a TypedCRUD[Annotation] for use in provider commands.
// It loads the REST config from the loader and constructs the annotations
// client. The caller may optionally pass base ListOptions (From/To/Tags/Limit)
// that will be applied to the ListFn.
func NewTypedCRUD(ctx context.Context, loader RESTConfigLoader, baseListOpts ListOptions) (*adapter.TypedCRUD[Annotation], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to load REST config for annotations: %w", err)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to create annotations client: %w", err)
	}

	return buildTypedCRUD(client, cfg.Namespace, baseListOpts), cfg, nil
}
