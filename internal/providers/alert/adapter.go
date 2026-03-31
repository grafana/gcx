package alert

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
)

const (
	// APIVersion is the API version for alert resources.
	APIVersion = "alerting.ext.grafana.app/v1alpha1"
	// RuleKind is the kind for alert rule resources.
	RuleKind = "AlertRule"
	// GroupKind is the kind for alert rule group resources.
	GroupKind = "AlertRuleGroup"
)

// RuleToResource converts a RuleStatus to a gcx Resource, wrapping the
// alert rule fields in a Kubernetes-style object envelope with apiVersion, kind,
// and metadata. The uid field is mapped to metadata.name.
func RuleToResource(rule RuleStatus, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal alert rule: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal alert rule to map: %w", err)
	}

	// Strip server-managed identifier from spec — it lives in metadata.name.
	delete(specMap, "uid")

	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       RuleKind,
		"metadata": map[string]any{
			"name":      rule.UID,
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// RuleFromResource converts a gcx Resource back to a RuleStatus.
// The UID is restored from metadata.name.
func RuleFromResource(res *resources.Resource) (*RuleStatus, error) {
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

	var rule RuleStatus
	if err := json.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to alert rule: %w", err)
	}

	// Restore UID from metadata.name.
	rule.UID = res.Raw.GetName()

	return &rule, nil
}

// GroupToResource converts a RuleGroup to a gcx Resource, wrapping the
// alert rule group fields in a Kubernetes-style object envelope.
// The name field is mapped to metadata.name.
func GroupToResource(group RuleGroup, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(group)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal alert rule group: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal alert rule group to map: %w", err)
	}

	// Strip the name from spec — it lives in metadata.name.
	delete(specMap, "name")

	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       GroupKind,
		"metadata": map[string]any{
			"name":      group.Name,
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// GroupFromResource converts a gcx Resource back to a RuleGroup.
// The Name is restored from metadata.name.
func GroupFromResource(res *resources.Resource) (*RuleGroup, error) {
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

	var group RuleGroup
	if err := json.Unmarshal(data, &group); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to alert rule group: %w", err)
	}

	// Restore Name from metadata.name.
	group.Name = res.Raw.GetName()

	return &group, nil
}
