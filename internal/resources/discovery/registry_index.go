package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/grafana/grafanactl/internal/resources"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// RegistryIndex is an index with information
// about the resources & groups supported by the API server.
type RegistryIndex struct {
	lock sync.RWMutex

	shortGroups       map[string]string
	longGroups        map[string]struct{}
	preferredVersions map[string]schema.GroupVersion
	descriptors       map[schema.GroupVersion]resources.Descriptors
	singularNames     map[string][]schema.GroupKind
	pluralNames       map[string][]schema.GroupKind
	kindNames         map[string][]schema.GroupKind
}

// NewRegistryIndex creates a new empty index.
func NewRegistryIndex() RegistryIndex {
	return RegistryIndex{
		shortGroups:       make(map[string]string),
		longGroups:        make(map[string]struct{}),
		preferredVersions: make(map[string]schema.GroupVersion),
		descriptors:       make(map[schema.GroupVersion]resources.Descriptors),
		kindNames:         make(map[string][]schema.GroupKind),
		singularNames:     make(map[string][]schema.GroupKind),
		pluralNames:       make(map[string][]schema.GroupKind),
	}
}

// GetDescriptors returns all resource Descriptors from the registry index.
func (r *RegistryIndex) GetDescriptors() resources.Descriptors {
	r.lock.RLock()
	defer r.lock.RUnlock()

	res := make(resources.Descriptors, 0, len(r.descriptors))
	for _, descs := range r.descriptors {
		res = append(res, descs...)
	}

	return res
}

// GetPreferredVersions returns all preferred versions of the API groups.
func (r *RegistryIndex) GetPreferredVersions() resources.Descriptors {
	r.lock.RLock()
	defer r.lock.RUnlock()

	res := make(resources.Descriptors, 0, len(r.preferredVersions))
	for _, gv := range r.preferredVersions {
		desc, ok := r.descriptors[gv]
		if !ok {
			// TODO: this should never happen.
			// panic?
			continue
		}

		res = append(res, desc...)
	}

	return res
}

// RegisterStatic registers a provider descriptor with its aliases into the index.
// This is used for provider-backed resource types that are not discovered via /apis
// but are instead statically declared by providers.
func (r *RegistryIndex) RegisterStatic(desc resources.Descriptor, aliases []string) {
	r.lock.Lock()
	defer r.lock.Unlock()

	gv := desc.GroupVersion

	// Register the group (short and long forms).
	r.longGroups[gv.Group] = struct{}{}
	shortName := makeShortName(gv.Group)
	r.shortGroups[shortName] = gv.Group

	// Register preferred version (first registration wins).
	if _, ok := r.preferredVersions[gv.Group]; !ok {
		r.preferredVersions[gv.Group] = gv
	}

	// Add the descriptor to the group version.
	r.descriptors[gv] = append(r.descriptors[gv], desc)

	// Register the kind name.
	gk := schema.GroupKind{Group: gv.Group, Kind: desc.Kind}
	r.kindNames[desc.Kind] = append(r.kindNames[desc.Kind], gk)

	// Register singular name.
	if desc.Singular != "" {
		r.singularNames[desc.Singular] = append(r.singularNames[desc.Singular], gk)
	}

	// Register plural name.
	if desc.Plural != "" {
		r.pluralNames[desc.Plural] = append(r.pluralNames[desc.Plural], gk)
	}

	// Register aliases as additional singular name lookups.
	for _, alias := range aliases {
		if alias == desc.Singular || alias == desc.Plural || alias == desc.Kind {
			continue // Already registered above.
		}
		r.singularNames[alias] = append(r.singularNames[alias], gk)
	}
}

// LookupPartialGVK returns a descriptor for the provided partial GVK.
// It tries to find the most precise match based on the information in the partial GVK.
// (i.e. it will try to scope down to the exact group & version if provided and fall back to the preferred version if not).
// If no group is provided, it will return the first group that supports the resource.
func (r *RegistryIndex) LookupPartialGVK(gvk resources.PartialGVK) (resources.Descriptor, bool) {
	groupKindCandidates, ok := r.getKindCandidates(gvk.Resource)
	if !ok {
		return resources.Descriptor{}, false
	}

	desc, ok := r.filterCandidates(groupKindCandidates, gvk.Group, gvk.Version)
	if !ok {
		return resources.Descriptor{}, false
	}

	return desc, true
}

// LookupAllVersionsForPartialGVK returns all descriptors for the provided partial GVK.
// This is useful when you want to get all supported versions of a resource type.
// If group is provided, it will only return versions for that group.
// If version is provided, it will only return that specific version (same as LookupPartialGVK).
// If neither group nor version are provided, it will return all versions of all groups that support the resource.
func (r *RegistryIndex) LookupAllVersionsForPartialGVK(gvk resources.PartialGVK) (resources.Descriptors, bool) {
	groupKindCandidates, ok := r.getKindCandidates(gvk.Resource)
	if !ok {
		return nil, false
	}

	descs, ok := r.filterAllCandidates(groupKindCandidates, gvk.Group, gvk.Version)
	if !ok {
		return nil, false
	}

	return descs, true
}

// Update updates the registry index from the provided API groups and resources.
func (r *RegistryIndex) Update(ctx context.Context, groups []*metav1.APIGroup, list []*metav1.APIResourceList) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	logger := logging.FromContext(ctx)

	var (
		// API group indices
		shortGroups       = make(map[string]string)
		longGroups        = make(map[string]struct{})
		preferredVersions = make(map[string]schema.GroupVersion)
		descriptors       = make(map[schema.GroupVersion]resources.Descriptors)

		// API resource indices
		kindNames     = make(map[string][]schema.GroupKind)
		singularNames = make(map[string][]schema.GroupKind)
		pluralNames   = make(map[string][]schema.GroupKind)
	)

	// First pass: collect preferred versions and group versions.
	for _, group := range groups {
		name := group.Name
		longGroups[name] = struct{}{}

		shortName := makeShortName(name)
		shortGroups[shortName] = name

		if group.PreferredVersion.Version != "" {
			preferredVersions[name] = schema.GroupVersion{
				Group:   name,
				Version: group.PreferredVersion.Version,
			}
		}

		for _, version := range group.Versions {
			gv := schema.GroupVersion{
				Group:   name,
				Version: version.Version,
			}

			if _, ok := descriptors[gv]; ok {
				logger.Info(
					"inconsistent discovery response from the server - duplicate GroupVersion entry",
					"group", name,
					"version", version.Version,
				)

				return fmt.Errorf("duplicate group version: %s", gv.String())
			}

			descriptors[gv] = make(resources.Descriptors, 0)
		}
	}

	// Second pass: collect resources and put them into the descriptors.
	// Each descriptor will be associated with a group version.
	for _, list := range list {
		for _, res := range list.APIResources {
			gv, err := parseGroupVersion(list.GroupVersion)
			if err != nil {
				return err
			}

			// Check if the resource has a specified group,
			// if so, we'll use that group.
			if res.Group != "" {
				gv.Group = res.Group
			}

			// Same as above, but for the version.
			if res.Version != "" {
				gv.Version = res.Version
			}

			desc, ok := descriptors[gv]
			if !ok {
				logger.Info(
					"inconsistent discovery response from the server - API resource is missing from API groups",
					"group", gv.Group,
					"version", gv.Version,
					"resource", res.Name,
				)

				return fmt.Errorf(
					"unexpected response from the server - resource is not listed as supported: %s",
					gv.String(),
				)
			}

			// TODO: filter out subresources
			logger.Debug("adding resource to discovery registry",
				"group", gv.Group,
				"version", gv.Version,
				"kind", res.Kind,
				"singular", res.SingularName,
				"plural", res.Name,
			)

			descriptors[gv] = append(desc, resources.Descriptor{
				GroupVersion: gv,
				Kind:         res.Kind,
				Plural:       res.Name,
				Singular:     res.SingularName,
			})

			if _, ok := kindNames[res.Kind]; !ok {
				kindNames[res.Kind] = make([]schema.GroupKind, 0)
			}

			kindNames[res.Kind] = append(kindNames[res.Kind], schema.GroupKind{
				Group: gv.Group,
				Kind:  res.Kind,
			})

			if _, ok := singularNames[res.SingularName]; !ok {
				singularNames[res.SingularName] = make([]schema.GroupKind, 0)
			}

			singularNames[res.SingularName] = append(singularNames[res.SingularName], schema.GroupKind{
				Group: gv.Group,
				Kind:  res.Kind,
			})

			if _, ok := pluralNames[res.Name]; !ok {
				pluralNames[res.Name] = make([]schema.GroupKind, 0)
			}

			pluralNames[res.Name] = append(pluralNames[res.Name], schema.GroupKind{
				Group: gv.Group,
				Kind:  res.Kind,
			})
		}
	}

	r.shortGroups = shortGroups
	r.longGroups = longGroups
	r.preferredVersions = preferredVersions
	r.descriptors = descriptors
	r.kindNames = kindNames
	r.singularNames = singularNames
	r.pluralNames = pluralNames

	return nil
}

func (r *RegistryIndex) getKindCandidates(resource string) ([]schema.GroupKind, bool) {
	// Collect candidates from all maps so that filterCandidates can
	// disambiguate by group/version. Without merging, an alias in
	// singularNames can shadow a legitimate plural in pluralNames
	// (e.g. alert's "rules" alias shadows KG's "rules" plural).
	var candidates []schema.GroupKind
	seen := make(map[schema.GroupKind]struct{})

	for _, m := range []map[string][]schema.GroupKind{
		r.kindNames,
		r.singularNames,
		r.pluralNames,
	} {
		if gks, ok := m[resource]; ok {
			for _, gk := range gks {
				if _, dup := seen[gk]; !dup {
					seen[gk] = struct{}{}
					candidates = append(candidates, gk)
				}
			}
		}
	}

	return candidates, len(candidates) > 0
}

func (r *RegistryIndex) filterCandidates(
	groupKindCandidates []schema.GroupKind, group, version string,
) (resources.Descriptor, bool) {
	var groupVersion schema.GroupVersion

	if group != "" {
		found := false

		// Check if the group was provided in the short form.
		if g, ok := r.shortGroups[group]; ok {
			groupVersion.Group = g
			found = true
		}

		// Check if the group was provided in the long form.
		if _, ok := r.longGroups[group]; ok {
			groupVersion.Group = group
			found = true
		}

		if !found {
			return resources.Descriptor{}, false
		}
	}

	// Check that the group is supported.
	var kind string
	for _, gk := range groupKindCandidates {
		// TODO: there's an issue here that if we have resource & version (but not group)
		// we end up choosing the first group that supports the resource, but this might not be what we want.
		// e.g. the lookup could be for resource = Foo, version = v1
		// And we use group = Bar but that group might not support v1
		// (whereas some other group does support v1 and has the same resource Foo)
		if groupVersion.Group == "" {
			groupVersion.Group = gk.Group
		}

		if groupVersion.Group == gk.Group {
			kind = gk.Kind
			break
		}
	}

	// If somehow we didn't find a kind, we can't proceed.
	// This could happen if the group & kind combination doesn't exist.
	if kind == "" {
		return resources.Descriptor{}, false
	}

	if version != "" {
		groupVersion.Version = version
	} else if gv, ok := r.preferredVersions[groupVersion.Group]; ok {
		groupVersion.Version = gv.Version
	} else {
		// Somehow the group is supported, but we don't have a preferred version for it.
		// TODO: panic?
		return resources.Descriptor{}, false
	}

	descs, ok := r.descriptors[groupVersion]
	if !ok {
		return resources.Descriptor{}, false
	}

	for _, desc := range descs {
		if desc.Kind == kind {
			return desc, true
		}
	}

	return resources.Descriptor{}, false
}

// filterAllCandidates returns all descriptors for the given candidates based on group and version constraints.
// If version is specified, it returns only that version.
// If group is specified but not version, it returns all versions for that group.
// If neither is specified, it returns all versions for all groups.
func (r *RegistryIndex) filterAllCandidates(
	groupKindCandidates []schema.GroupKind, group, version string,
) (resources.Descriptors, bool) {
	// If a specific version is requested, use the existing single-descriptor logic
	if version != "" {
		desc, ok := r.filterCandidates(groupKindCandidates, group, version)
		if !ok {
			return nil, false
		}
		return resources.Descriptors{desc}, true
	}

	var targetGroups []string
	if group != "" {
		// Check if the group was provided in the short form.
		if g, ok := r.shortGroups[group]; ok {
			targetGroups = []string{g}
		} else if _, ok := r.longGroups[group]; ok {
			// Check if the group was provided in the long form.
			targetGroups = []string{group}
		} else {
			return nil, false
		}
	} else {
		// No group specified, collect all groups that support this resource
		groupSet := make(map[string]struct{})
		for _, gk := range groupKindCandidates {
			groupSet[gk.Group] = struct{}{}
		}
		for g := range groupSet {
			targetGroups = append(targetGroups, g)
		}
	}

	var result resources.Descriptors
	for _, targetGroup := range targetGroups {
		// Find the kind for this group
		var kind string
		for _, gk := range groupKindCandidates {
			if gk.Group == targetGroup {
				kind = gk.Kind
				break
			}
		}
		if kind == "" {
			continue
		}

		// Find all versions for this group/kind combination
		for gv, descs := range r.descriptors {
			if gv.Group != targetGroup {
				continue
			}
			for _, desc := range descs {
				if desc.Kind == kind {
					result = append(result, desc)
					break
				}
			}
		}
	}

	if len(result) == 0 {
		return nil, false
	}

	return result, true
}

func makeShortName(name string) string {
	return strings.Split(name, ".")[0]
}

func parseGroupVersion(src string) (schema.GroupVersion, error) {
	parts := strings.Split(src, "/")

	if len(parts) != 2 {
		return schema.GroupVersion{}, fmt.Errorf("invalid group version: %s", src)
	}

	if parts[0] == "" {
		return schema.GroupVersion{}, fmt.Errorf("invalid group version: %s", src)
	}

	if parts[1] == "" {
		return schema.GroupVersion{}, fmt.Errorf("invalid group version: %s", src)
	}

	return schema.GroupVersion{
		Group:   parts[0],
		Version: parts[1],
	}, nil
}
