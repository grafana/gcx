package alert

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

		return &RulesAdapter{
			client:    client,
			namespace: cfg.Namespace,
		}, nil
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

		return &GroupsAdapter{
			client:    client,
			namespace: cfg.Namespace,
		}, nil
	}
}

// RulesAdapter bridges the alert.Client to the grafanactl resources pipeline for alert rules.
// The alert API is read-only; Create, Update, and Delete return errors.ErrUnsupported.
type RulesAdapter struct {
	client    *Client
	namespace string
}

var _ adapter.ResourceAdapter = &RulesAdapter{}

// Descriptor returns the resource descriptor this adapter serves.
func (a *RulesAdapter) Descriptor() resources.Descriptor {
	return staticRulesDescriptor
}

// Aliases returns short names for selector resolution.
func (a *RulesAdapter) Aliases() []string {
	return staticRulesAliases
}

// List returns all alert rule resources as unstructured objects.
func (a *RulesAdapter) List(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	resp, err := a.client.List(ctx, ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list alert rules: %w", err)
	}

	result := &unstructured.UnstructuredList{}
	for _, group := range resp.Data.Groups {
		for _, rule := range group.Rules {
			res, err := RuleToResource(rule, a.namespace)
			if err != nil {
				return nil, fmt.Errorf("failed to convert alert rule %q to resource: %w", rule.UID, err)
			}
			result.Items = append(result.Items, res.Object)
		}
	}

	return result, nil
}

// Get returns a single alert rule resource by UID.
func (a *RulesAdapter) Get(ctx context.Context, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	rule, err := a.client.GetRule(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get alert rule %q: %w", name, err)
	}

	res, err := RuleToResource(*rule, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to convert alert rule %q to resource: %w", name, err)
	}

	obj := res.ToUnstructured()
	return &obj, nil
}

// Create is not supported for alert rules. The alert API is read-only.
func (a *RulesAdapter) Create(_ context.Context, _ *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	return nil, errors.ErrUnsupported
}

// Update is not supported for alert rules. The alert API is read-only.
func (a *RulesAdapter) Update(_ context.Context, _ *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, errors.ErrUnsupported
}

// Delete is not supported for alert rules. The alert API is read-only.
func (a *RulesAdapter) Delete(_ context.Context, _ string, _ metav1.DeleteOptions) error {
	return errors.ErrUnsupported
}

// GroupsAdapter bridges the alert.Client to the grafanactl resources pipeline for alert rule groups.
// The alert API is read-only; Create, Update, and Delete return errors.ErrUnsupported.
type GroupsAdapter struct {
	client    *Client
	namespace string
}

var _ adapter.ResourceAdapter = &GroupsAdapter{}

// Descriptor returns the resource descriptor this adapter serves.
func (a *GroupsAdapter) Descriptor() resources.Descriptor {
	return staticGroupsDescriptor
}

// Aliases returns short names for selector resolution.
func (a *GroupsAdapter) Aliases() []string {
	return staticGroupsAliases
}

// List returns all alert rule group resources as unstructured objects.
func (a *GroupsAdapter) List(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	groups, err := a.client.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list alert rule groups: %w", err)
	}

	result := &unstructured.UnstructuredList{}
	for _, group := range groups {
		res, err := GroupToResource(group, a.namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to convert alert rule group %q to resource: %w", group.Name, err)
		}
		result.Items = append(result.Items, res.Object)
	}

	return result, nil
}

// Get returns a single alert rule group resource by name.
func (a *GroupsAdapter) Get(ctx context.Context, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	group, err := a.client.GetGroup(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get alert rule group %q: %w", name, err)
	}

	res, err := GroupToResource(*group, a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to convert alert rule group %q to resource: %w", name, err)
	}

	obj := res.ToUnstructured()
	return &obj, nil
}

// Create is not supported for alert rule groups. The alert API is read-only.
func (a *GroupsAdapter) Create(_ context.Context, _ *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	return nil, errors.ErrUnsupported
}

// Update is not supported for alert rule groups. The alert API is read-only.
func (a *GroupsAdapter) Update(_ context.Context, _ *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, errors.ErrUnsupported
}

// Delete is not supported for alert rule groups. The alert API is read-only.
func (a *GroupsAdapter) Delete(_ context.Context, _ string, _ metav1.DeleteOptions) error {
	return errors.ErrUnsupported
}
