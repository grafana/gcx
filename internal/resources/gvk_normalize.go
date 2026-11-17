package resources

import (
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GVKNormalizer maps a manifest's GroupVersionKind to a canonical one. It
// returns the canonical GVK and true when it applies, or the zero GVK and false
// otherwise.
//
// Normalizers exist so a manifest can carry an alternate API group that still
// routes to a statically-registered adapter. The motivating case is datasources:
// Grafana serves a per-plugin group ({pluginID}.datasource.grafana.app) but gcx
// registers a single canonical descriptor (datasource.grafana.app) backed by the
// type-agnostic legacy REST API, so every per-plugin group must collapse onto it.
type GVKNormalizer func(schema.GroupVersionKind) (schema.GroupVersionKind, bool)

//nolint:gochecknoglobals // Self-registration pattern (same as adapter natural-key registry).
var (
	gvkNormalizerMu sync.RWMutex
	gvkNormalizers  []GVKNormalizer
	groupAliases    = make(map[schema.GroupVersionKind]schema.GroupVersionKind)
)

// RegisterGroupAliases keeps compatibility scoped to an exact kind and version.
func RegisterGroupAliases(canonical schema.GroupVersionKind, groups []string) {
	gvkNormalizerMu.Lock()
	defer gvkNormalizerMu.Unlock()
	for _, group := range groups {
		alias := canonical
		alias.Group = group
		groupAliases[alias] = canonical
	}
}

// MatchesGroupAlias accepts full groups and their first DNS label in selectors.
func MatchesGroupAlias(canonical schema.GroupVersionKind, group string) bool {
	gvkNormalizerMu.RLock()
	defer gvkNormalizerMu.RUnlock()
	for alias, target := range groupAliases {
		if target == canonical && (group == alias.Group || group == strings.SplitN(alias.Group, ".", 2)[0]) {
			return true
		}
	}
	return false
}

// RegisterGVKNormalizer registers a normalizer. Providers call this during init
// to make manifests with alternate API groups route to their canonical adapter.
func RegisterGVKNormalizer(fn GVKNormalizer) {
	gvkNormalizerMu.Lock()
	defer gvkNormalizerMu.Unlock()
	gvkNormalizers = append(gvkNormalizers, fn)
}

// NormalizeGVK resolves exact group aliases first, then applies normalizers in
// registration order and returns the first matching GVK. If neither applies,
// the input GVK is returned unchanged.
func NormalizeGVK(gvk schema.GroupVersionKind) schema.GroupVersionKind {
	if target := normalizeGroupAlias(gvk); target != gvk {
		return target
	}
	gvkNormalizerMu.RLock()
	defer gvkNormalizerMu.RUnlock()
	for _, fn := range gvkNormalizers {
		if canonical, ok := fn(gvk); ok {
			return canonical
		}
	}
	return gvk
}

func normalizeGroupAlias(gvk schema.GroupVersionKind) schema.GroupVersionKind {
	gvkNormalizerMu.RLock()
	defer gvkNormalizerMu.RUnlock()
	if target, ok := groupAliases[gvk]; ok {
		return target
	}
	return gvk
}

// resetGVKNormalizers clears the registry (for testing only).
func resetGVKNormalizers() {
	gvkNormalizerMu.Lock()
	defer gvkNormalizerMu.Unlock()
	gvkNormalizers = nil
	clear(groupAliases)
}
