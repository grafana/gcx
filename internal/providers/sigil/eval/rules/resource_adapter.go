package rules

import (
	"context"
	"encoding/json"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// StaticDescriptor returns the resource descriptor for Sigil evaluation rules.
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "sigil.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     "EvalRule",
		Singular: "evalrule",
		Plural:   "evalrules",
	}
}

// RuleSchema returns a JSON Schema for the EvalRule resource type.
func RuleSchema() json.RawMessage {
	return adapter.SchemaFromType[eval.RuleDefinition](StaticDescriptor())
}

func ruleStripFields() []string {
	return []string{
		"rule_id", "tenant_id",
		"created_by", "updated_by", "deleted_at", "created_at", "updated_at",
	}
}

// NewTypedCRUD creates a TypedCRUD for Sigil evaluation rules.
func NewTypedCRUD(ctx context.Context) (*adapter.TypedCRUD[eval.RuleDefinition], string, error) {
	var loader providers.ConfigLoader
	loader.SetContextName(internalconfig.ContextNameFromCtx(ctx))

	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load REST config for Sigil rules: %w", err)
	}

	base, err := sigilhttp.NewClient(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create Sigil HTTP client: %w", err)
	}
	client := NewClient(base)

	crud := &adapter.TypedCRUD[eval.RuleDefinition]{
		ListFn: func(ctx context.Context, limit int64) ([]eval.RuleDefinition, error) {
			items, err := client.List(ctx)
			if err != nil {
				return nil, err
			}
			if limit > 0 && int64(len(items)) > limit {
				items = items[:limit]
			}
			return items, nil
		},
		GetFn: func(ctx context.Context, name string) (*eval.RuleDefinition, error) {
			return client.Get(ctx, name)
		},
		CreateFn: func(ctx context.Context, item *eval.RuleDefinition) (*eval.RuleDefinition, error) {
			return client.Create(ctx, item)
		},
		UpdateFn: func(ctx context.Context, name string, item *eval.RuleDefinition) (*eval.RuleDefinition, error) {
			return client.Update(ctx, name, item)
		},
		DeleteFn:    client.Delete,
		Namespace:   cfg.Namespace,
		StripFields: ruleStripFields(),
		Descriptor:  StaticDescriptor(),
	}
	return crud, cfg.Namespace, nil
}

// NewLazyFactory returns an adapter.Factory for Sigil evaluation rules.
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewTypedCRUD(ctx)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// specToUnstructured converts a RuleDefinition to a K8s-style unstructured
// envelope, stripping server-managed fields so JSON/YAML output matches the
// resources pipeline.
func specToUnstructured(item eval.RuleDefinition, namespace string) (unstructured.Unstructured, error) {
	data, err := json.Marshal(item)
	if err != nil {
		return unstructured.Unstructured{}, fmt.Errorf("failed to marshal rule: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return unstructured.Unstructured{}, fmt.Errorf("failed to unmarshal rule spec: %w", err)
	}

	for _, f := range ruleStripFields() {
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
