package remote

import (
	"context"
	"sync"

	"github.com/grafana/gcx/internal/resources"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// naturalKeyCache caches List results per GVK to avoid redundant API calls
// when matching multiple resources of the same type by natural key.
type naturalKeyCache struct {
	lister PushLister
	mu     sync.Mutex
	cache  map[schema.GroupVersionKind]*unstructured.UnstructuredList
}

// newNaturalKeyCache creates a cache backed by the given lister.
// If lister is nil, all lookups return nil (natural key matching is disabled).
func newNaturalKeyCache(lister PushLister) *naturalKeyCache {
	return &naturalKeyCache{
		lister: lister,
		cache:  make(map[schema.GroupVersionKind]*unstructured.UnstructuredList),
	}
}

// list returns the cached list for the given descriptor's GVK, fetching it
// on first access. Returns nil if the cache has no lister.
func (c *naturalKeyCache) list(ctx context.Context, desc resources.Descriptor) (*unstructured.UnstructuredList, error) {
	if c.lister == nil {
		return nil, nil //nolint:nilnil // nil list signals natural key matching is disabled.
	}

	gvk := desc.GroupVersionKind()

	c.mu.Lock()
	if result, ok := c.cache[gvk]; ok {
		c.mu.Unlock()
		return result, nil
	}
	c.mu.Unlock()

	// Fetch outside the lock to avoid blocking concurrent pushes of other GVKs.
	result, err := c.lister.List(ctx, desc, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	// Double-check: another goroutine may have populated it while we were fetching.
	if existing, ok := c.cache[gvk]; ok {
		c.mu.Unlock()
		return existing, nil
	}
	c.cache[gvk] = result
	c.mu.Unlock()

	return result, nil
}
