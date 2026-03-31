package reports

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
)

const (
	// APIVersion is the API version for SLO Report resources.
	APIVersion = "slo.ext.grafana.app/v1alpha1"
	// Kind is the kind for SLO Report resources.
	Kind = "Report"
)

// ToResource converts a Report to a gcx Resource, wrapping the report fields
// in a Kubernetes-style object envelope with apiVersion, kind, and metadata.
// The uuid field is mapped to metadata.name and stripped from the spec.
func ToResource(report Report, namespace string) (*resources.Resource, error) {
	// Marshal the Report to JSON and unmarshal into a generic map
	data, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal report: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal report to map: %w", err)
	}

	// Strip server-managed fields from the spec
	delete(specMap, "uuid")

	// Build the Kubernetes-style object envelope
	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name":      report.UUID,
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// FromResource converts a gcx Resource back to a Report.
// The UUID is restored from metadata.name.
func FromResource(res *resources.Resource) (*Report, error) {
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

	// Marshal spec to JSON and unmarshal to Report
	data, err := json.Marshal(specMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec: %w", err)
	}

	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to report: %w", err)
	}

	// Restore UUID from metadata.name
	report.UUID = res.Raw.GetName()

	return &report, nil
}

// FileNamer returns a function that produces a file path for a Report resource.
// The path follows the pattern: Report/{name}.{format}.
func FileNamer(outputFormat string) func(*resources.Resource) string {
	return func(res *resources.Resource) string {
		return fmt.Sprintf("Report/%s.%s", res.Raw.GetName(), outputFormat)
	}
}
