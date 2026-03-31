package alert

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// staticRulesDescriptor is the resource descriptor for alert rule resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticRulesDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "alerting.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     "AlertRule",
	Singular: "alertrule",
	Plural:   "alertrules",
}

// staticGroupsDescriptor is the resource descriptor for alert rule group resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticGroupsDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "alerting.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     "AlertRuleGroup",
	Singular: "alertrulegroup",
	Plural:   "alertrulegroups",
}

// alertRuleSchema returns a JSON Schema for the AlertRule resource type.
func alertRuleSchema() json.RawMessage {
	return adapter.SchemaFromType[RuleStatus](staticRulesDescriptor)
}

// alertRuleGroupSchema returns a JSON Schema for the AlertRuleGroup resource type.
func alertRuleGroupSchema() json.RawMessage {
	return adapter.SchemaFromType[RuleGroup](staticGroupsDescriptor)
}

// NewRulesAdapterFactory returns a lazy adapter.Factory for alert rules.
// The factory captures the GrafanaConfigLoader and constructs the client on first invocation.
func NewRulesAdapterFactory(loader GrafanaConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewTypedCRUDRules(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// NewGroupsAdapterFactory returns a lazy adapter.Factory for alert rule groups.
// The factory captures the GrafanaConfigLoader and constructs the client on first invocation.
func NewGroupsAdapterFactory(loader GrafanaConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewTypedCRUDGroups(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// NewTypedCRUD[RuleStatus] creates a TypedCRUD for alert rules.
func NewTypedCRUDRules(ctx context.Context, loader GrafanaConfigLoader) (*adapter.TypedCRUD[RuleStatus], string, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load REST config for alert rules: %w", err)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create alert client: %w", err)
	}

	crud := &adapter.TypedCRUD[RuleStatus]{
		ListFn: func(ctx context.Context) ([]RuleStatus, error) {
			resp, err := client.List(ctx, ListOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to list alert rules: %w", err)
			}
			var rules []RuleStatus
			for _, group := range resp.Data.Groups {
				rules = append(rules, group.Rules...)
			}
			return rules, nil
		},
		GetFn: func(ctx context.Context, name string) (*RuleStatus, error) {
			rule, err := client.GetRule(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("failed to get alert rule %q: %w", name, err)
			}
			return rule, nil
		},
		Namespace:   cfg.Namespace,
		StripFields: []string{"uid"},
		Descriptor:  staticRulesDescriptor,
	}
	return crud, cfg.Namespace, nil
}

// NewTypedCRUD[RuleGroup] creates a TypedCRUD for alert rule groups.
func NewTypedCRUDGroups(ctx context.Context, loader GrafanaConfigLoader) (*adapter.TypedCRUD[RuleGroup], string, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load REST config for alert groups: %w", err)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create alert client: %w", err)
	}

	crud := &adapter.TypedCRUD[RuleGroup]{
		ListFn: func(ctx context.Context) ([]RuleGroup, error) {
			groups, err := client.ListGroups(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to list alert rule groups: %w", err)
			}
			return groups, nil
		},
		GetFn: func(ctx context.Context, name string) (*RuleGroup, error) {
			group, err := client.GetGroup(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("failed to get alert rule group %q: %w", name, err)
			}
			return group, nil
		},
		Namespace:   cfg.Namespace,
		StripFields: []string{"name"},
		Descriptor:  staticGroupsDescriptor,
	}
	return crud, cfg.Namespace, nil
}
