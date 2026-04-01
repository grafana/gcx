package checks

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// Slug helpers — thin wrappers around adapter.SlugifyName / adapter.ExtractInt64IDFromSlug.
// slugifyJob returns "check" (not "resource") for empty input to match the SM domain.

func slugifyJob(job string) string {
	s := adapter.SlugifyName(job)
	if s == "resource" {
		return "check"
	}
	return s
}

func extractIDFromSlug(name string) (int64, bool) { return adapter.ExtractInt64IDFromSlug(name) }

// ToResource converts an API Check + probe map to a K8s-envelope Resource.
// probeNames maps probe ID → name for display in the YAML file.
// Server-managed fields (id, tenantId, created, modified, channels) are stripped.
func ToResource(check Check, namespace string, probeNames map[int64]string) (*resources.Resource, error) {
	// Resolve probe IDs to names for the YAML spec.
	probeNameList := make([]string, 0, len(check.Probes))
	for _, id := range check.Probes {
		name, ok := probeNames[id]
		if !ok {
			name = strconv.FormatInt(id, 10) // fallback to numeric string if name unknown
		}
		probeNameList = append(probeNameList, name)
	}

	spec := CheckSpec{
		Job:              check.Job,
		Target:           check.Target,
		Frequency:        check.Frequency,
		Offset:           check.Offset,
		Timeout:          check.Timeout,
		Enabled:          check.Enabled,
		Labels:           check.Labels,
		Settings:         check.Settings,
		Probes:           probeNameList,
		BasicMetricsOnly: check.BasicMetricsOnly,
		AlertSensitivity: check.AlertSensitivity,
	}

	// Marshal spec to generic map for the K8s envelope.
	specData, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("marshalling check spec: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(specData, &specMap); err != nil {
		return nil, fmt.Errorf("unmarshalling check spec to map: %w", err)
	}

	// Embed the numeric check ID as a suffix in the name (e.g. "web-check-8127").
	// This guarantees uniqueness even when two checks share the same Job string
	// (e.g. same job targeting different URLs). The suffix also lets FromResource,
	// Get, and Delete recover the numeric API ID from the name alone.
	name := slugifyJob(check.Job)
	if check.ID != 0 {
		name = name + "-" + strconv.FormatInt(check.ID, 10)
	}
	metadata := map[string]any{
		"name":      name,
		"namespace": namespace,
	}
	// Also store in metadata.uid as a secondary source for files written by
	// older versions that used uid for ID recovery.
	if check.ID != 0 {
		metadata["uid"] = strconv.FormatInt(check.ID, 10)
	}

	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata":   metadata,
		"spec":       specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// FromResource converts a K8s-envelope Resource back to a CheckSpec.
// The numeric check ID is read from metadata.name (if set and parseable).
// Probe names in spec.probes are left as names — callers resolve them to IDs.
func FromResource(res *resources.Resource) (*CheckSpec, int64, error) {
	obj := res.Object.Object

	specRaw, ok := obj["spec"]
	if !ok {
		return nil, 0, errors.New("resource has no spec field")
	}

	specMap, ok := specRaw.(map[string]any)
	if !ok {
		return nil, 0, errors.New("resource spec is not a map")
	}

	specData, err := json.Marshal(specMap)
	if err != nil {
		return nil, 0, fmt.Errorf("marshalling spec: %w", err)
	}

	var spec CheckSpec
	if err := json.Unmarshal(specData, &spec); err != nil {
		return nil, 0, fmt.Errorf("unmarshalling spec to CheckSpec: %w", err)
	}

	// Recover the numeric check ID. Priority order:
	//  1. metadata.uid  — set by older ToResource versions
	//  2. metadata.name — current "slug-<id>" format, or legacy pure-numeric name
	// 0 means "create new check".
	var id int64
	if uid := res.Raw.GetUID(); uid != "" {
		if parsed, err := strconv.ParseInt(string(uid), 10, 64); err == nil {
			id = parsed
		}
	}
	if id == 0 {
		if name := res.Raw.GetName(); name != "" {
			if parsed, ok := extractIDFromSlug(name); ok {
				id = parsed
			}
		}
	}

	return &spec, id, nil
}

// SpecToCheck converts a CheckSpec + resolved probe IDs to an API Check struct.
// tenantID must be fetched from the server before calling this.
func SpecToCheck(spec *CheckSpec, id, tenantID int64, probeIDs []int64) Check {
	return Check{
		ID:               id,
		TenantID:         tenantID,
		Job:              spec.Job,
		Target:           spec.Target,
		Frequency:        spec.Frequency,
		Offset:           spec.Offset,
		Timeout:          spec.Timeout,
		Enabled:          spec.Enabled,
		Labels:           spec.Labels,
		Settings:         spec.Settings,
		Probes:           probeIDs,
		BasicMetricsOnly: spec.BasicMetricsOnly,
		AlertSensitivity: spec.AlertSensitivity,
	}
}

// FileNamer returns a function that produces the file path for a check resource.
// Path convention: checks/{id}.yaml.
func FileNamer(outputFormat string) func(*resources.Resource) string {
	return func(res *resources.Resource) string {
		return fmt.Sprintf("checks/%s.%s", res.Raw.GetName(), outputFormat)
	}
}
