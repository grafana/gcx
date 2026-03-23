package kg

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

const (
	// APIVersion is the API version for KG rule resources.
	APIVersion = "kg.ext.grafana.app/v1alpha1"
	// Kind is the kind for KG rule resources.
	Kind = "Rule"
)

// staticDescriptor is the resource descriptor for KG rule resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "kg.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     Kind,
	Singular: "rule",
	Plural:   "rules",
}

// staticAliases are the short aliases for KG rule resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticAliases = []string{"kg-rules", "kg-rule", "kgrule"}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	loader := &providers.ConfigLoader{}
	adapter.Register(adapter.Registration{
		Factory:    NewAdapterFactory(loader),
		Descriptor: staticDescriptor,
		Aliases:    staticAliases,
		GVK:        staticDescriptor.GroupVersionKind(),
		Schema:     ruleSchema(),
		Example:    ruleExample(),
	})
}

// ruleSchema returns a JSON Schema for the KG Rule resource type.
func ruleSchema() json.RawMessage {
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
					"name":        map[string]any{"type": "string"},
					"expr":        map[string]any{"type": "string"},
					"record":      map[string]any{"type": "string"},
					"alert":       map[string]any{"type": "string"},
					"labels":      map[string]any{"type": "object"},
					"annotations": map[string]any{"type": "object"},
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

// ruleExample returns an example KG Rule manifest as JSON.
func ruleExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "my-custom-rule",
		},
		"spec": map[string]any{
			"name":   "my-custom-rule",
			"expr":   "sum(rate(http_requests_total[5m])) by (service)",
			"record": "service:http_requests:rate5m",
			"labels": map[string]any{
				"team": "platform",
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

		return &ResourceAdapter{
			client:    client,
			namespace: cfg.Namespace,
		}, nil
	}
}

// ResourceAdapter bridges the KG Rules Client to the grafanactl resources pipeline.
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

// List returns all KG rules as unstructured objects.
func (a *ResourceAdapter) List(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	rules, err := a.client.ListRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("kg: list rules: %w", err)
	}

	result := &unstructured.UnstructuredList{}
	for _, rule := range rules {
		res, err := RuleToResource(rule, a.namespace)
		if err != nil {
			return nil, fmt.Errorf("kg: convert rule %q to resource: %w", rule.Name, err)
		}
		result.Items = append(result.Items, res.Object)
	}

	return result, nil
}

// Get returns a single KG rule by name.
func (a *ResourceAdapter) Get(ctx context.Context, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	rule, err := a.client.GetRule(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("kg: get rule %q: %w", name, err)
	}

	res, err := RuleToResource(*rule, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("kg: convert rule %q to resource: %w", name, err)
	}

	obj := res.ToUnstructured()
	return &obj, nil
}

// Create creates a KG rule by uploading YAML prom rules.
// The KG API only supports bulk upload of rules, so individual create
// is not directly supported — the caller should use the rules create command instead.
func (a *ResourceAdapter) Create(_ context.Context, _ *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	return nil, errors.New("kg: individual rule creation is not supported; use 'kg rules create -f <file>' for bulk upload")
}

// Update is not supported for KG rules — they are replaced in bulk.
func (a *ResourceAdapter) Update(_ context.Context, _ *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, errors.New("kg: individual rule update is not supported; use 'kg rules create -f <file>' for bulk replace")
}

// Delete is not supported for individual KG rules — use 'kg rules delete' for bulk clear.
func (a *ResourceAdapter) Delete(_ context.Context, _ string, _ metav1.DeleteOptions) error {
	return errors.New("kg: individual rule deletion is not supported; use 'kg rules delete' to clear all rules")
}

// RuleToResource converts a KG Rule to a grafanactl Resource.
func RuleToResource(rule Rule, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(rule)
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
			"name":      rule.Name,
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// RuleFromResource converts a grafanactl Resource back to a KG Rule.
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

	var rule Rule
	if err := json.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to rule: %w", err)
	}

	if rule.Name == "" {
		rule.Name = res.Raw.GetName()
	}

	return &rule, nil
}
