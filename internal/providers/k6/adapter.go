package k6

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/grafana/gcx/internal/resources"
)

const (
	// APIGroup is the API group for k6 resources.
	APIGroup = "k6.ext.grafana.app"
	// APIVersionStr is the API version string for k6 resources.
	APIVersionStr = "v1alpha1"
	// APIVersion is the full API version for k6 resources.
	APIVersion = APIGroup + "/" + APIVersionStr
	// Kind is the kind for k6 project resources.
	Kind = "Project"
)

// ---------------------------------------------------------------------------
// Generic helpers
// ---------------------------------------------------------------------------

// toResourceGeneric marshals a typed struct to a Resource envelope.
// stripFields lists JSON keys to remove from the spec (e.g. "id").
// nameValue is set as metadata.name.
func toResourceGeneric(item any, kind, nameValue, namespace string, stripFields []string) (*resources.Resource, error) {
	data, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s: %w", kind, err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s to map: %w", kind, err)
	}

	for _, f := range stripFields {
		delete(specMap, f)
	}

	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name":      nameValue,
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// fromResourceGeneric extracts a typed struct from a Resource's spec.
func fromResourceGeneric[T any](res *resources.Resource) (*T, error) {
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

	var item T
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec: %w", err)
	}

	return &item, nil
}

// restoreIntID parses metadata.name as an int and returns it (or 0 if unparseable).
func restoreIntID(res *resources.Resource) int {
	name := res.Raw.GetName()
	if name == "" {
		return 0
	}
	id, _ := strconv.Atoi(name)
	return id
}

// ---------------------------------------------------------------------------
// Project
// ---------------------------------------------------------------------------

// ToResource converts a Project to a gcx Resource.
func ToResource(p Project, namespace string) (*resources.Resource, error) {
	return toResourceGeneric(p, "Project", strconv.Itoa(p.ID), namespace, []string{"id"})
}

// FromResource converts a gcx Resource back to a Project.
func FromResource(res *resources.Resource) (*Project, error) {
	p, err := fromResourceGeneric[Project](res)
	if err != nil {
		return nil, err
	}
	p.ID = restoreIntID(res)
	return p, nil
}

// ---------------------------------------------------------------------------
// LoadTest
// ---------------------------------------------------------------------------

// LoadTestToResource converts a LoadTest to a gcx Resource.
func LoadTestToResource(lt LoadTest, namespace string) (*resources.Resource, error) {
	return toResourceGeneric(lt, "LoadTest", strconv.Itoa(lt.ID), namespace, []string{"id"})
}

// LoadTestFromResource converts a gcx Resource back to a LoadTest.
func LoadTestFromResource(res *resources.Resource) (*LoadTest, error) {
	lt, err := fromResourceGeneric[LoadTest](res)
	if err != nil {
		return nil, err
	}
	lt.ID = restoreIntID(res)
	return lt, nil
}

// ---------------------------------------------------------------------------
// Schedule
// ---------------------------------------------------------------------------

// ScheduleToResource converts a Schedule to a gcx Resource.
func ScheduleToResource(s Schedule, namespace string) (*resources.Resource, error) {
	return toResourceGeneric(s, "Schedule", strconv.Itoa(s.ID), namespace, []string{"id"})
}

// ScheduleFromResource converts a gcx Resource back to a Schedule.
func ScheduleFromResource(res *resources.Resource) (*Schedule, error) {
	s, err := fromResourceGeneric[Schedule](res)
	if err != nil {
		return nil, err
	}
	s.ID = restoreIntID(res)
	return s, nil
}

// ---------------------------------------------------------------------------
// EnvVar
// ---------------------------------------------------------------------------

// EnvVarToResource converts an EnvVar to a gcx Resource.
func EnvVarToResource(ev EnvVar, namespace string) (*resources.Resource, error) {
	return toResourceGeneric(ev, "EnvVar", strconv.Itoa(ev.ID), namespace, []string{"id"})
}

// EnvVarFromResource converts a gcx Resource back to an EnvVar.
func EnvVarFromResource(res *resources.Resource) (*EnvVar, error) {
	ev, err := fromResourceGeneric[EnvVar](res)
	if err != nil {
		return nil, err
	}
	ev.ID = restoreIntID(res)
	return ev, nil
}

// ---------------------------------------------------------------------------
// LoadZone
// ---------------------------------------------------------------------------

// LoadZoneToResource converts a LoadZone to a gcx Resource.
// The Name field (not the numeric ID) is used as metadata.name.
func LoadZoneToResource(lz LoadZone, namespace string) (*resources.Resource, error) {
	return toResourceGeneric(lz, "LoadZone", lz.Name, namespace, []string{"id"})
}

// LoadZoneFromResource converts a gcx Resource back to a LoadZone.
func LoadZoneFromResource(res *resources.Resource) (*LoadZone, error) {
	lz, err := fromResourceGeneric[LoadZone](res)
	if err != nil {
		return nil, err
	}
	// Restore the Name from metadata.name (not numeric ID).
	lz.Name = res.Raw.GetName()
	// Also try to restore the numeric ID.
	lz.ID = restoreIntID(res)
	return lz, nil
}
