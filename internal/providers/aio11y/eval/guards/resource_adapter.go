package guards

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// StaticDescriptor returns the resource descriptor for Agent Observability hook rules (guards).
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "sigil.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     "HookRule",
		Singular: "hookrule",
		Plural:   "hookrules",
	}
}

// HookRuleSchema returns a JSON Schema for the HookRule resource type.
func HookRuleSchema() json.RawMessage {
	return adapter.SchemaFromType[eval.HookRuleDefinition](StaticDescriptor())
}

func hookRuleStripFields() []string {
	return []string{
		"rule_id", "tenant_id",
		"created_by", "updated_by", "deleted_at", "created_at", "updated_at",
	}
}

// NewTypedCRUD creates a TypedCRUD for Agent Observability hook rules.
// The loader carries the command's --config selection; adapter factories pass
// a zero-value loader and inherit the selection threaded through ctx by the
// resources command.
func NewTypedCRUD(ctx context.Context, loader *providers.ConfigLoader) (*adapter.TypedCRUD[eval.HookRuleDefinition], string, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load REST config for Agent Observability hook rules: %w", err)
	}

	base, err := aio11yhttp.NewClient(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create Agent Observability HTTP client: %w", err)
	}
	client := NewClient(base)

	crud := &adapter.TypedCRUD[eval.HookRuleDefinition]{
		ListFn: adapter.LimitedListFn(client.List),
		GetFn: func(ctx context.Context, name string) (*eval.HookRuleDefinition, error) {
			return client.Get(ctx, name)
		},
		CreateFn: func(ctx context.Context, item *eval.HookRuleDefinition) (*eval.HookRuleDefinition, error) {
			return client.Create(ctx, item)
		},
		UpdateFn: func(ctx context.Context, name string, item *eval.HookRuleDefinition) (*eval.HookRuleDefinition, error) {
			return client.Update(ctx, name, item)
		},
		DeleteFn:    client.Delete,
		Namespace:   cfg.Namespace,
		StripFields: hookRuleStripFields(),
		Descriptor:  StaticDescriptor(),
	}
	return crud, cfg.Namespace, nil
}

// NewLazyFactory returns an adapter.Factory for Agent Observability hook rules.
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		var loader providers.ConfigLoader
		crud, _, err := NewTypedCRUD(ctx, &loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// specToUnstructured converts a HookRuleDefinition to a K8s-style unstructured
// envelope, stripping server-managed fields so JSON/YAML output matches the
// resources pipeline.
func specToUnstructured(item eval.HookRuleDefinition, namespace string) (unstructured.Unstructured, error) {
	data, err := json.Marshal(item)
	if err != nil {
		return unstructured.Unstructured{}, fmt.Errorf("failed to marshal hook rule: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return unstructured.Unstructured{}, fmt.Errorf("failed to unmarshal hook rule spec: %w", err)
	}

	for _, f := range hookRuleStripFields() {
		delete(specMap, f)
	}

	desc := StaticDescriptor()
	return unstructured.Unstructured{Object: map[string]any{
		"apiVersion": desc.GroupVersion.String(),
		"kind":       desc.Kind,
		"metadata": map[string]any{
			"name":      item.RuleID,
			"namespace": namespace,
		},
		"spec": specMap,
	}}, nil
}
