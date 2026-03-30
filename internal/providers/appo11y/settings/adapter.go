package settings

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
)

const (
	// APIVersion is the API version for Settings resources.
	APIVersion = "appo11y.ext.grafana.app/v1alpha1"
	// Kind is the kind for Settings resources.
	Kind = "Settings"
)

// ToResource converts a PluginSettings to a gcx Resource, wrapping the settings fields
// in a Kubernetes-style object envelope with apiVersion, kind, and metadata.
func ToResource(s PluginSettings, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PluginSettings: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal PluginSettings to map: %w", err)
	}

	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name":      s.GetResourceName(),
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// FromResource converts a gcx Resource back to a PluginSettings.
func FromResource(res *resources.Resource) (*PluginSettings, error) {
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

	var s PluginSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to PluginSettings: %w", err)
	}

	return &s, nil
}
