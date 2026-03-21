package oncall

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/grafanactl/internal/resources"
)

const (
	// APIGroup is the API group for all OnCall resources.
	APIGroup = "oncall.ext.grafana.app"
	// APIVersion is the full API version.
	APIVersion = APIGroup + "/v1alpha1"
	// Version is the API version.
	Version = "v1alpha1"
)

// fromResource converts a grafanactl Resource back to a typed OnCall object.
func fromResource[T any](res *resources.Resource) (*T, error) {
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

	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec: %w", err)
	}

	return &result, nil
}
