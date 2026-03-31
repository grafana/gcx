package checks

import (
	"context"
	"fmt"
	"strconv"

	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// staticDescriptor is the resource descriptor for SM Check resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "syntheticmonitoring.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     "Check",
	Singular: "check",
	Plural:   "checks",
}

// checkResource is an internal wrapper around CheckSpec that carries
// non-serialized metadata needed by TypedCRUD's MetadataFn.
// Unexported fields are ignored by json.Marshal, so the serialized output
// is identical to marshaling a plain CheckSpec.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type checkResource struct {
	CheckSpec

	name    string // pre-computed resource name (e.g., "web-check-1001")
	checkID int64  // numeric API check ID
}

// GetResourceName returns the pre-computed composite name (slug + check ID).
func (cr checkResource) GetResourceName() string { return cr.name }

// SetResourceName restores the composite name from metadata.
func (cr *checkResource) SetResourceName(name string) { cr.name = name }

// NewTypedCRUD creates a TypedCRUD for SM checks.
// It loads config via the provided Loader and returns both CRUD and config.
func NewTypedCRUD(ctx context.Context, loader smcfg.Loader) (*adapter.TypedCRUD[checkResource], string, error) {
	baseURL, token, namespace, err := loader.LoadSMConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load SM config for checks: %w", err)
	}

	checksClient := NewClient(baseURL, token)
	probesClient := probes.NewClient(baseURL, token)

	crud := &adapter.TypedCRUD[checkResource]{
		ListFn: func(ctx context.Context) ([]checkResource, error) {
			checkList, err := checksClient.List(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to list checks: %w", err)
			}

			nameMap, err := probeNameMap(ctx, probesClient)
			if err != nil {
				return nil, err
			}

			result := make([]checkResource, 0, len(checkList))
			for _, check := range checkList {
				result = append(result, checkToResource(check, nameMap))
			}

			return result, nil
		},

		GetFn: func(ctx context.Context, name string) (*checkResource, error) {
			id, ok := extractIDFromSlug(name)
			if !ok {
				return nil, fmt.Errorf("could not extract numeric check ID from name %q", name)
			}

			check, err := checksClient.Get(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("failed to get check %d: %w", id, err)
			}

			nameMap, err := probeNameMap(ctx, probesClient)
			if err != nil {
				return nil, err
			}

			cr := checkToResource(*check, nameMap)
			return &cr, nil
		},

		CreateFn: func(ctx context.Context, item *checkResource) (*checkResource, error) {
			tenant, err := checksClient.GetTenant(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get SM tenant: %w", err)
			}

			idMap, err := probeIDMap(ctx, probesClient)
			if err != nil {
				return nil, err
			}

			probeIDs, err := resolveProbeIDs(item.Probes, idMap)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve probe IDs for create: %w", err)
			}

			check := SpecToCheck(&item.CheckSpec, 0, tenant.ID, probeIDs)

			created, err := checksClient.Create(ctx, check)
			if err != nil {
				return nil, fmt.Errorf("failed to create check: %w", err)
			}

			nameMap := invertIDMap(idMap)
			cr := checkToResource(*created, nameMap)
			return &cr, nil
		},

		UpdateFn: func(ctx context.Context, name string, item *checkResource) (*checkResource, error) {
			tenant, err := checksClient.GetTenant(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get SM tenant: %w", err)
			}

			idMap, err := probeIDMap(ctx, probesClient)
			if err != nil {
				return nil, err
			}

			probeIDs, err := resolveProbeIDs(item.Probes, idMap)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve probe IDs for update: %w", err)
			}

			check := SpecToCheck(&item.CheckSpec, item.checkID, tenant.ID, probeIDs)

			updated, err := checksClient.Update(ctx, check)
			if err != nil {
				return nil, fmt.Errorf("failed to update check %d: %w", item.checkID, err)
			}

			nameMap := invertIDMap(idMap)
			cr := checkToResource(*updated, nameMap)
			return &cr, nil
		},

		DeleteFn: func(ctx context.Context, name string) error {
			id, ok := extractIDFromSlug(name)
			if !ok {
				return fmt.Errorf("could not extract numeric check ID from name %q", name)
			}

			return checksClient.Delete(ctx, id)
		},

		Namespace: namespace,

		MetadataFn: func(cr checkResource) map[string]any {
			if cr.checkID != 0 {
				return map[string]any{
					"uid": strconv.FormatInt(cr.checkID, 10),
				}
			}
			return nil
		},

		Descriptor: staticDescriptor,
	}

	return crud, namespace, nil
}

// NewAdapterFactory returns a lazy adapter.Factory for SM checks.
// The factory captures the smcfg.Loader and constructs clients on first invocation.
func NewAdapterFactory(loader smcfg.Loader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// StaticDescriptor returns the static descriptor for SM Check resources.
// Used for registration without constructing an adapter instance.
func StaticDescriptor() resources.Descriptor {
	return staticDescriptor
}

// StaticGVK returns the static GroupVersionKind for SM Check resources.
func StaticGVK() schema.GroupVersionKind {
	return staticDescriptor.GroupVersionKind()
}

// checkToResource converts an API Check + probe name map into a checkResource.
func checkToResource(check Check, probeNames map[int64]string) checkResource {
	probeNameList := make([]string, 0, len(check.Probes))
	for _, id := range check.Probes {
		name, ok := probeNames[id]
		if !ok {
			name = strconv.FormatInt(id, 10)
		}
		probeNameList = append(probeNameList, name)
	}

	name := slugifyJob(check.Job)
	if check.ID != 0 {
		name = name + "-" + strconv.FormatInt(check.ID, 10)
	}

	return checkResource{
		CheckSpec: CheckSpec{
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
		},
		name:    name,
		checkID: check.ID,
	}
}

// probeNameMap fetches all probes and builds an ID->name map.
func probeNameMap(ctx context.Context, probesClient *probes.Client) (map[int64]string, error) {
	probeList, err := probesClient.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching probe list for name resolution: %w", err)
	}

	nameMap := make(map[int64]string, len(probeList))
	for _, p := range probeList {
		nameMap[p.ID] = p.Name
	}

	return nameMap, nil
}

// probeIDMap fetches all probes and builds a name->ID map.
func probeIDMap(ctx context.Context, probesClient *probes.Client) (map[string]int64, error) {
	probeList, err := probesClient.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching probe list for ID resolution: %w", err)
	}

	idMap := make(map[string]int64, len(probeList))
	for _, p := range probeList {
		idMap[p.Name] = p.ID
	}

	return idMap, nil
}

// resolveProbeIDs converts probe names to IDs using the provided map.
// Returns an error if any name is not found in the map.
func resolveProbeIDs(names []string, idMap map[string]int64) ([]int64, error) {
	ids := make([]int64, 0, len(names))
	for _, name := range names {
		id, ok := idMap[name]
		if !ok {
			return nil, fmt.Errorf("probe %q not found", name)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// invertIDMap converts a name->ID map to an ID->name map.
func invertIDMap(idMap map[string]int64) map[int64]string {
	nameMap := make(map[int64]string, len(idMap))
	for name, id := range idMap {
		nameMap[id] = name
	}
	return nameMap
}
