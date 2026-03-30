package overrides

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
)

const (
	// APIVersion is the API version for Overrides resources.
	APIVersion = "appo11y.ext.grafana.app/v1alpha1"
	// Kind is the kind for Overrides resources.
	Kind = "Overrides"
	// ETagAnnotation is the annotation key used to round-trip the ETag.
	ETagAnnotation = "appo11y.ext.grafana.app/etag"
)

// ToResource converts a MetricsGeneratorConfig to a gcx Resource, wrapping the
// config fields in a Kubernetes-style object envelope. The ETag is stored in
// metadata.annotations so it survives a pull/push round-trip.
func ToResource(cfg MetricsGeneratorConfig, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal overrides config: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal overrides config to map: %w", err)
	}

	metadata := map[string]any{
		"name":      cfg.GetResourceName(),
		"namespace": namespace,
	}

	if etag := cfg.ETag(); etag != "" {
		metadata["annotations"] = map[string]any{
			ETagAnnotation: etag,
		}
	}

	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata":   metadata,
		"spec":       specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// FromResource converts a gcx Resource back to a MetricsGeneratorConfig.
// The ETag is restored from metadata.annotations if present.
func FromResource(res *resources.Resource) (*MetricsGeneratorConfig, error) {
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

	var cfg MetricsGeneratorConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to overrides config: %w", err)
	}

	// Restore ETag from annotations if present.
	if etag := res.Raw.GetAnnotations()[ETagAnnotation]; etag != "" {
		cfg.SetETag(etag)
	}

	return &cfg, nil
}
