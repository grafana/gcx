package k6

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/grafana/grafanactl/internal/resources"
)

const (
	// APIVersion is the API version for k6 project resources.
	APIVersion = "k6.ext.grafana.app/v1alpha1"
	// Kind is the kind for k6 project resources.
	Kind = "Project"
)

// ToResource converts a Project to a grafanactl Resource, wrapping the project
// fields in a Kubernetes-style object envelope with apiVersion, kind, and metadata.
// The ID field is mapped to metadata.name and stripped from the spec.
func ToResource(p Project, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal project: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal project to map: %w", err)
	}

	// Strip the ID from spec — it lives in metadata.name.
	delete(specMap, "id")

	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name":      strconv.Itoa(p.ID),
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// FromResource converts a grafanactl Resource back to a Project.
// The ID is restored from metadata.name.
func FromResource(res *resources.Resource) (*Project, error) {
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

	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to project: %w", err)
	}

	// Restore ID from metadata.name.
	name := res.Raw.GetName()
	if name != "" {
		id, err := strconv.Atoi(name)
		if err == nil {
			p.ID = id
		}
	}

	return &p, nil
}
