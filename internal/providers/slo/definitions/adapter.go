package definitions

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
)

const (
	// APIVersion is the API version for SLO resources.
	APIVersion = "slo.ext.grafana.app/v1alpha1"
	// Kind is the kind for SLO resources.
	Kind = "SLO"
)

// ToResource converts an Slo to a gcx Resource, wrapping the SLO fields
// in a Kubernetes-style object envelope with apiVersion, kind, and metadata.
// The uuid field is mapped to metadata.name and stripped from the spec.
// The readOnly field is also stripped from the spec.
func ToResource(slo Slo, namespace string) (*resources.Resource, error) {
	// Marshal the Slo to JSON and unmarshal into a generic map
	data, err := json.Marshal(slo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SLO: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal SLO to map: %w", err)
	}

	// Strip server-managed fields from the spec
	delete(specMap, "uuid")
	delete(specMap, "readOnly")

	// Build the Kubernetes-style object envelope
	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name":      slo.UUID,
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// FromResource converts a gcx Resource back to an Slo.
// The UUID is restored from metadata.name.
func FromResource(res *resources.Resource) (*Slo, error) {
	obj := res.Object.Object

	// Get the spec from the resource
	specRaw, ok := obj["spec"]
	if !ok {
		return nil, errors.New("resource has no spec field")
	}

	specMap, ok := specRaw.(map[string]any)
	if !ok {
		return nil, errors.New("resource spec is not a map")
	}

	// Marshal spec to JSON and unmarshal to Slo
	data, err := json.Marshal(specMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec: %w", err)
	}

	var slo Slo
	if err := json.Unmarshal(data, &slo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to SLO: %w", err)
	}

	// Restore UUID from metadata.name
	slo.UUID = res.Raw.GetName()

	return &slo, nil
}

// FileNamer returns a function that produces a file path for an SLO resource.
// The path follows the pattern: SLO/{name}.{format}.
func FileNamer(outputFormat string) func(*resources.Resource) string {
	return func(res *resources.Resource) string {
		return fmt.Sprintf("SLO/%s.%s", res.Raw.GetName(), outputFormat)
	}
}
