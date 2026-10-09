package irm

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
)

const (
	// APIVersion is the API version for incident resources.
	IncidentAPIVersion = "incident.ext.grafana.app/v1alpha1"
	// Kind is the kind for incident resources.
	IncidentKind = "Incident"
)

// ToResource converts an Incident to a gcx Resource, wrapping the incident
// fields in a Kubernetes-style object envelope with apiVersion, kind, and metadata.
// The incidentID field is mapped to metadata.name and stripped from the spec.
//
// The strip list matches TypedCRUD.StripFields in
// incidents_resource_adapter.go. The provider commands convert here, and the
// resources commands convert through the adapter, so a key that one path
// removes and the other keeps makes the two outputs disagree.
func ToResource(inc Incident, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(inc)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal incident: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal incident to map: %w", err)
	}

	// The identifier lives in metadata.name. severityID goes with it: the
	// identifier has precedence over the severity label in the client, so a
	// manifest that carries both makes an edit of the label unreachable.
	for _, field := range incidentStripFields {
		delete(specMap, field)
	}

	obj := map[string]any{
		"apiVersion": IncidentAPIVersion,
		"kind":       IncidentKind,
		"metadata": map[string]any{
			"name":      inc.IncidentID,
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// FromResource converts a gcx Resource back to an Incident.
// The IncidentID is restored from metadata.name.
func FromResource(res *resources.Resource) (*Incident, error) {
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

	var inc Incident
	if err := json.Unmarshal(data, &inc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to incident: %w", err)
	}

	// Restore IncidentID from metadata.name.
	inc.IncidentID = res.Raw.GetName()

	return &inc, nil
}
