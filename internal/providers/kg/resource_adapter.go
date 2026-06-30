package kg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// APIVersion is the API version for KG rule resources.
	APIVersion = "kg.ext.grafana.app/v1alpha1"
	// Kind is the kind for KG rule resources.
	Kind = "Rule"
)

//nolint:gochecknoglobals // Static descriptors used in init() self-registration pattern.
var (
	kgGroupVersion = schema.GroupVersion{Group: "kg.ext.grafana.app", Version: "v1alpha1"}

	staticDescriptor = resources.Descriptor{
		GroupVersion: kgGroupVersion,
		Kind:         Kind,
		Singular:     "rule",
		Plural:       "rules",
	}

	scopeDescriptor = resources.Descriptor{
		GroupVersion: kgGroupVersion,
		Kind:         "Scope",
		Singular:     "scope",
		Plural:       "scopes",
	}

	modelRulesDescriptor = resources.Descriptor{
		GroupVersion: kgGroupVersion,
		Kind:         "ModelRules",
		Singular:     "modelrules",
		Plural:       "modelrules",
	}
)

// Descriptor accessors for use in tests and registration.

// RuleDescriptor returns the resource descriptor for KG rules.
func RuleDescriptor() resources.Descriptor { return staticDescriptor }

// ScopeDescriptor returns the resource descriptor for KG scopes.
func ScopeDescriptor() resources.Descriptor { return scopeDescriptor }

// ModelRulesDescriptor returns the resource descriptor for KG model rules.
func ModelRulesDescriptor() resources.Descriptor { return modelRulesDescriptor }

// RuleSchema returns a JSON Schema for the KG Rule resource type.
func RuleSchema() json.RawMessage {
	s := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/KGRule",
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
					"name": map[string]any{"type": "string"},
					"groups": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":     map[string]any{"type": "string"},
								"interval": map[string]any{"type": "string"},
								"rules": map[string]any{
									"type": "array",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"record":          map[string]any{"type": "string"},
											"alert":           map[string]any{"type": "string"},
											"expr":            map[string]any{"type": "string"},
											"for":             map[string]any{"type": "string"},
											"labels":          map[string]any{"type": "object"},
											"annotations":     map[string]any{"type": "object"},
											"disableInGroups": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										},
									},
								},
							},
							"required": []string{"name"},
						},
					},
				},
				"required": []string{"name"},
			},
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("kg: failed to marshal schema: %v", err))
	}
	return b
}

// RuleExample returns an example KG Rule manifest as JSON.
func RuleExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "my-custom-rule",
		},
		"spec": map[string]any{
			"name": "my-custom-rule",
			"groups": []map[string]any{
				{
					"name":     "my-group",
					"interval": "30s",
					"rules": []map[string]any{
						{
							"record": "service:http_requests:rate5m",
							"expr":   "sum(rate(http_requests_total[5m])) by (service)",
							"labels": map[string]any{
								"team": "platform",
							},
						},
					},
				},
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("kg: failed to marshal example: %v", err))
	}
	return b
}

// RESTConfigLoader can load a NamespacedRESTConfig from the active context.
type RESTConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (internalconfig.NamespacedRESTConfig, error)
}

// NewAdapterFactory returns a lazy adapter.Factory for KG rules.
func NewAdapterFactory(loader RESTConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("kg: failed to load REST config: %w", err)
		}

		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("kg: failed to create client: %w", err)
		}

		crud := &adapter.TypedCRUD[Rule]{
			ListFn: adapter.LimitedListFn(client.ListRules),
			GetFn: func(ctx context.Context, name string) (*Rule, error) {
				return client.GetRule(ctx, name)
			},
			DeleteFn:   client.DeleteRule,
			Namespace:  cfg.Namespace,
			Descriptor: staticDescriptor,
		}
		return crud.AsAdapter(), nil
	}
}

// NewTypedCRUD creates a TypedCRUD for KG rules.
func NewTypedCRUD(ctx context.Context, loader RESTConfigLoader) (*adapter.TypedCRUD[Rule], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("kg: failed to load REST config: %w", err)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("kg: failed to create client: %w", err)
	}

	crud := &adapter.TypedCRUD[Rule]{
		ListFn: adapter.LimitedListFn(client.ListRules),
		GetFn: func(ctx context.Context, name string) (*Rule, error) {
			return client.GetRule(ctx, name)
		},
		DeleteFn:   client.DeleteRule,
		Namespace:  cfg.Namespace,
		Descriptor: staticDescriptor,
	}
	return crud, cfg, nil
}

// RuleToResource converts a KG Rule to a gcx Resource.
func RuleToResource(rf Rule, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(rf)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rule: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rule to map: %w", err)
	}

	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name":      rf.Name,
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// ---------------------------------------------------------------------------
// Scope adapter
// ---------------------------------------------------------------------------

// ScopeSchema returns a JSON Schema for the KG Scope resource type.
func ScopeSchema() json.RawMessage { return mustSchema("KGScope", "Scope", scopeSpecSchema()) }

func scopeSpecSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":   map[string]any{"type": "string"},
			"values": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"name"},
	}
}

// NewScopeAdapterFactory returns a lazy adapter.Factory for KG scopes.
func NewScopeAdapterFactory(loader RESTConfigLoader) adapter.Factory {
	return newListOnlyFactory[Scope](loader, scopeDescriptor,
		func(client *Client, ctx context.Context) ([]Scope, error) {
			scopeMap, err := client.ListEntityScopes(ctx, 0, 0)
			if err != nil {
				return nil, err
			}
			result := make([]Scope, 0, len(scopeMap))
			for name, values := range scopeMap {
				result = append(result, Scope{Name: name, Values: values})
			}
			return result, nil
		})
}

// ---------------------------------------------------------------------------
// ModelRules adapter
// ---------------------------------------------------------------------------

// ModelRulesSchema returns a JSON Schema for the KG ModelRules resource type.
func ModelRulesSchema() json.RawMessage {
	return mustSchema("KGModelRules", "ModelRules", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":      map[string]any{"type": "string"},
			"entities":  map[string]any{"type": "array"},
			"relations": map[string]any{"type": "array"},
			"managedBy": map[string]any{"type": "string"},
		},
		"required": []string{"name"},
	})
}

// NewModelRulesAdapterFactory returns a lazy adapter.Factory for KG model rules.
// CreateFn and UpdateFn are intentionally omitted until typed apply lands;
// `gcx resources push` will not work for model rules yet.
func NewModelRulesAdapterFactory(loader RESTConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("kg: failed to load REST config: %w", err)
		}
		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("kg: failed to create client: %w", err)
		}
		crud := &adapter.TypedCRUD[ModelRules]{
			ListFn: adapter.LimitedListFn(client.ListModelRules),
			GetFn: func(ctx context.Context, name string) (*ModelRules, error) {
				return client.GetModelRules(ctx, name)
			},
			DeleteFn:   client.DeleteModelRules,
			Namespace:  cfg.Namespace,
			Descriptor: modelRulesDescriptor,
		}
		return crud.AsAdapter(), nil
	}
}

// ModelRulesToResource converts a KG ModelRules to a gcx Resource.
func ModelRulesToResource(m ModelRules, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal model rules: %w", err)
	}
	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal model rules to map: %w", err)
	}
	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       "ModelRules",
		"metadata": map[string]any{
			"name":      m.Name,
			"namespace": namespace,
		},
		"spec": specMap,
	}
	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// ModelRulesFromResource converts a gcx Resource back to a KG ModelRules.
func ModelRulesFromResource(res *resources.Resource) (*ModelRules, error) {
	obj := res.Object.Object
	specRaw, ok := obj["spec"]
	if !ok {
		return nil, errors.New("resource has no spec field")
	}
	specMap, ok := specRaw.(map[string]any)
	if !ok {
		return nil, errors.New("resource spec is not a map")
	}
	data, err := json.Marshal(specMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec: %w", err)
	}
	var m ModelRules
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to model rules: %w", err)
	}
	if m.Name == "" {
		m.Name = res.Raw.GetName()
	}
	return &m, nil
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// newListOnlyFactory creates a factory for read-only (list-only) resource types.
// GetFn is nil, so TypedCRUD falls back to list + client-side name filtering.
func newListOnlyFactory[T adapter.ResourceNamer](
	loader RESTConfigLoader,
	desc resources.Descriptor,
	listFn func(client *Client, ctx context.Context) ([]T, error),
) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("kg: failed to load REST config: %w", err)
		}
		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("kg: failed to create client: %w", err)
		}
		crud := &adapter.TypedCRUD[T]{
			ListFn: adapter.LimitedListFn(func(ctx context.Context) ([]T, error) {
				return listFn(client, ctx)
			}),
			Namespace:  cfg.Namespace,
			Descriptor: desc,
		}
		return crud.AsAdapter(), nil
	}
}

// mustSchema builds a standard KG resource JSON Schema envelope.
func mustSchema(id, kind string, specSchema map[string]any) json.RawMessage {
	s := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/" + id,
		"type":    "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": APIVersion},
			"kind":       map[string]any{"type": "string", "const": kind},
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      map[string]any{"type": "string"},
					"namespace": map[string]any{"type": "string"},
				},
			},
			"spec": specSchema,
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("kg: failed to marshal schema: %v", err))
	}
	return b
}

// RuleFromResource converts a gcx Resource back to a KG Rule.
func RuleFromResource(res *resources.Resource) (*Rule, error) {
	obj := res.Object.Object

	specRaw, ok := obj["spec"]
	if !ok {
		return nil, errors.New("resource has no spec field")
	}

	specMap, ok := specRaw.(map[string]any)
	if !ok {
		return nil, errors.New("resource spec is not a map")
	}

	data, err := json.Marshal(specMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec: %w", err)
	}

	var rf Rule
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to rule: %w", err)
	}

	if rf.Name == "" {
		rf.Name = res.Raw.GetName()
	}

	return &rf, nil
}
