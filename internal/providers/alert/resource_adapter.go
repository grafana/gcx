package alert

import (
	"context"
	"fmt"

	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
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

// staticRulesAliases are the short aliases for alert rule resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticRulesAliases = []string{"rules"}

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

// staticGroupsAliases are the short aliases for alert rule group resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticGroupsAliases = []string{"groups"}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	loader := &providers.ConfigLoader{}
	adapter.Register(adapter.Registration{
		Factory:    NewRulesAdapterFactory(loader),
		Descriptor: staticRulesDescriptor,
		Aliases:    staticRulesAliases,
		GVK:        staticRulesDescriptor.GroupVersionKind(),
	})
	adapter.Register(adapter.Registration{
		Factory:    NewGroupsAdapterFactory(loader),
		Descriptor: staticGroupsDescriptor,
		Aliases:    staticGroupsAliases,
		GVK:        staticGroupsDescriptor.GroupVersionKind(),
	})
}

// NewRulesAdapterFactory returns a lazy adapter.Factory for alert rules.
// The factory captures the GrafanaConfigLoader and constructs the client on first invocation.
func NewRulesAdapterFactory(loader GrafanaConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load REST config for alert rules adapter: %w", err)
		}

		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create alert client for rules adapter: %w", err)
		}

		crud := &adapter.TypedCRUD[RuleStatus]{
			NameFn: func(r RuleStatus) string { return r.UID },
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
			// CreateFn, UpdateFn, DeleteFn all nil (read-only).
			Namespace:   cfg.Namespace,
			StripFields: []string{"uid"},
			Descriptor:  staticRulesDescriptor,
			Aliases:     staticRulesAliases,
		}
		return crud.AsAdapter(), nil
	}
}

// NewGroupsAdapterFactory returns a lazy adapter.Factory for alert rule groups.
// The factory captures the GrafanaConfigLoader and constructs the client on first invocation.
func NewGroupsAdapterFactory(loader GrafanaConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load REST config for alert groups adapter: %w", err)
		}

		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create alert client for groups adapter: %w", err)
		}

		crud := &adapter.TypedCRUD[RuleGroup]{
			NameFn: func(g RuleGroup) string { return g.Name },
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
			// CreateFn, UpdateFn, DeleteFn all nil (read-only).
			Namespace:   cfg.Namespace,
			StripFields: []string{"name"},
			Descriptor:  staticGroupsDescriptor,
			Aliases:     staticGroupsAliases,
		}
		return crud.AsAdapter(), nil
	}
}
