package probes

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/grafana/gcx/internal/resources"
)

// ToResource converts a Probe to a K8s-envelope Resource.
// Server-managed fields (tenantId, created, modified, onlineChange) are stripped.
func ToResource(probe Probe, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(probe)
	if err != nil {
		return nil, fmt.Errorf("marshalling probe: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("unmarshalling probe to map: %w", err)
	}

	// Strip server-managed or display-only fields from the spec.
	delete(specMap, "id")
	delete(specMap, "tenantId")
	delete(specMap, "created")
	delete(specMap, "modified")
	delete(specMap, "onlineChange")
	delete(specMap, "online")
	delete(specMap, "version")

	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name":      strconv.FormatInt(probe.ID, 10),
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}
