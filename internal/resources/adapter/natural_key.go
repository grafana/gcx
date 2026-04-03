package adapter

import (
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// NaturalKeyExtractor extracts a content-based identity key from a resource.
// Returns the key and true if extraction succeeded, or ("", false) if the
// resource lacks the required fields.
type NaturalKeyExtractor func(obj *unstructured.Unstructured) (string, bool)

//nolint:gochecknoglobals // Self-registration pattern (same as adapter.registry).
var (
	naturalKeyMu       sync.RWMutex
	naturalKeyRegistry = make(map[schema.GroupVersionKind]NaturalKeyExtractor)
)

// RegisterNaturalKey registers a natural key extractor for a GVK.
// Providers call this during init to enable cross-stack identity matching.
func RegisterNaturalKey(gvk schema.GroupVersionKind, fn NaturalKeyExtractor) {
	naturalKeyMu.Lock()
	defer naturalKeyMu.Unlock()
	naturalKeyRegistry[gvk] = fn
}

// GetNaturalKeyExtractor returns the registered extractor for a GVK, or nil.
func GetNaturalKeyExtractor(gvk schema.GroupVersionKind) NaturalKeyExtractor {
	naturalKeyMu.RLock()
	defer naturalKeyMu.RUnlock()
	return naturalKeyRegistry[gvk]
}

// SpecFieldKey returns a NaturalKeyExtractor that reads named fields from the
// resource's "spec" map, slugifies each value, and joins them with "/".
// Returns ("", false) if any field is missing or empty.
func SpecFieldKey(fields ...string) NaturalKeyExtractor {
	return func(obj *unstructured.Unstructured) (string, bool) {
		spec, ok := obj.Object["spec"].(map[string]any)
		if !ok {
			return "", false
		}

		parts := make([]string, 0, len(fields))
		for _, field := range fields {
			val, ok := spec[field]
			if !ok {
				return "", false
			}
			s, ok := val.(string)
			if !ok || s == "" {
				return "", false
			}
			parts = append(parts, SlugifyName(s))
		}

		return strings.Join(parts, "/"), true
	}
}

// resetNaturalKeyRegistry clears the registry (for testing only).
func resetNaturalKeyRegistry() {
	naturalKeyMu.Lock()
	defer naturalKeyMu.Unlock()
	naturalKeyRegistry = make(map[schema.GroupVersionKind]NaturalKeyExtractor)
}
