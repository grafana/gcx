package evaluators

import (
	"context"
	"encoding/json"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// StaticDescriptor returns the resource descriptor for AI Observability evaluators.
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "sigil.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     "Evaluator",
		Singular: "evaluator",
		Plural:   "evaluators",
	}
}

// EvaluatorSchema returns a JSON Schema for the Evaluator resource type.
func EvaluatorSchema() json.RawMessage {
	return adapter.SchemaFromType[eval.EvaluatorDefinition](StaticDescriptor())
}

func evalStripFields() []string {
	return []string{
		"evaluator_id", "tenant_id", "is_predefined",
		"source_template_id", "source_template_version",
		"created_by", "updated_by", "deleted_at", "created_at", "updated_at",
	}
}

// NewTypedCRUD creates a TypedCRUD for AI Observability evaluators.
func NewTypedCRUD(ctx context.Context) (*adapter.TypedCRUD[eval.EvaluatorDefinition], string, error) {
	var loader providers.ConfigLoader
	loader.SetContextName(internalconfig.ContextNameFromCtx(ctx))

	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load REST config for AI Observability evaluators: %w", err)
	}

	base, err := aio11yhttp.NewClient(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create AI Observability HTTP client: %w", err)
	}
	client := NewClient(base)

	crud := &adapter.TypedCRUD[eval.EvaluatorDefinition]{
		ListFn: adapter.LimitedListFn(client.List),
		GetFn: func(ctx context.Context, name string) (*eval.EvaluatorDefinition, error) {
			return client.Get(ctx, name)
		},
		CreateFn: func(ctx context.Context, item *eval.EvaluatorDefinition) (*eval.EvaluatorDefinition, error) {
			return client.Create(ctx, item)
		},
		// POST is upsert, so Update uses the same endpoint.
		UpdateFn: func(ctx context.Context, _ string, item *eval.EvaluatorDefinition) (*eval.EvaluatorDefinition, error) {
			return client.Create(ctx, item)
		},
		DeleteFn:    client.Delete,
		Namespace:   cfg.Namespace,
		StripFields: evalStripFields(),
		Descriptor:  StaticDescriptor(),
	}
	return crud, cfg.Namespace, nil
}

// NewLazyFactory returns an adapter.Factory for AI Observability evaluators.
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewTypedCRUD(ctx)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// specToUnstructured converts an EvaluatorDefinition to a K8s-style unstructured
// envelope, stripping server-managed fields so JSON/YAML output matches the
// resources pipeline.
func specToUnstructured(item eval.EvaluatorDefinition, namespace string) (unstructured.Unstructured, error) {
	data, err := json.Marshal(item)
	if err != nil {
		return unstructured.Unstructured{}, fmt.Errorf("failed to marshal evaluator: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return unstructured.Unstructured{}, fmt.Errorf("failed to unmarshal evaluator spec: %w", err)
	}

	for _, f := range evalStripFields() {
		delete(specMap, f)
	}

	desc := StaticDescriptor()
	return unstructured.Unstructured{Object: map[string]any{
		"apiVersion": desc.GroupVersion.String(),
		"kind":       desc.Kind,
		"metadata": map[string]any{
			"name":      item.EvaluatorID,
			"namespace": namespace,
		},
		"spec": specMap,
	}}, nil
}
