package remote

import (
	"context"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// findByNaturalKey attempts to find an existing remote resource that matches
// the local resource by content-based natural key. Returns the matching remote
// resource, including its manager fields, if found.
//
// This enables cross-stack push: when Get-by-metadata.name returns NotFound
// (because the server-generated ID from Stack A doesn't exist on Stack B),
// we fall back to matching by natural key (e.g., SLO name, check job+target).
func findByNaturalKey(
	ctx context.Context,
	cache *naturalKeyCache,
	desc resources.Descriptor,
	localObj *unstructured.Unstructured,
) (*unstructured.Unstructured, error) {
	// 1. Get the extractor for this GVK.
	extractor := adapter.GetNaturalKeyExtractor(desc.GroupVersionKind())
	if extractor == nil {
		return nil, nil //nolint:nilnil // A nil resource means no match.
	}

	// 2. Extract the local resource's natural key.
	localKey, ok := extractor(localObj)
	if !ok {
		return nil, nil //nolint:nilnil // A nil resource means no match.
	}

	// 3. Get the cached list of remote resources.
	remoteList, err := cache.list(ctx, desc)
	if err != nil {
		return nil, err
	}
	if remoteList == nil {
		return nil, nil //nolint:nilnil // A nil resource means no match.
	}

	// 4. Find a remote resource with the same natural key.
	for i := range remoteList.Items {
		remoteKey, ok := extractor(&remoteList.Items[i])
		if ok && remoteKey == localKey {
			return &remoteList.Items[i], nil
		}
	}

	return nil, nil //nolint:nilnil // A nil resource means no match.
}
