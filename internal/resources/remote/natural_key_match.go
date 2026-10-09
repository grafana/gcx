package remote

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/grafana-app-sdk/logging"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// findByNaturalKey attempts to find an existing remote resource that matches
// the local resource by content-based natural key. Returns the remote resource's
// object if found, retaining destination provenance for write checks.
//
// This enables cross-stack push: when Get-by-metadata.name returns NotFound
// (because the server-generated ID from Stack A doesn't exist on Stack B),
// we fall back to matching by natural key (e.g., SLO name, check job+target).
func findByNaturalKey( //nolint:nonamedreturns // Named returns document the match contract.
	ctx context.Context,
	cache *naturalKeyCache,
	desc resources.Descriptor,
	localObj *unstructured.Unstructured,
) (matched *unstructured.Unstructured, found bool, err error) {
	// 1. Get the extractor for this GVK.
	extractor := adapter.GetNaturalKeyExtractor(desc.GroupVersionKind())
	nativePolicy := extractor == nil && resources.HasServerDerivedName(desc.GroupVersionKind())
	if extractor == nil && !nativePolicy {
		return nil, false, nil
	}
	if nativePolicy {
		// Local manifests are strict; unreadable server items are skipped below.
		if _, _, err := resources.NativeIdentity(localObj); err != nil {
			return nil, false, err
		}
		extractor = func(obj *unstructured.Unstructured) (string, bool) {
			key, _, err := resources.NativeIdentity(obj)
			if err != nil {
				logging.FromContext(ctx).Debug("Skipping remote resource with invalid native identity", "name", obj.GetName(), logs.Err(err))
				return "", false
			}
			return key, true
		}
	}

	// 2. Extract the local resource's natural key.
	localKey, ok := extractor(localObj)
	if !ok {
		return nil, false, nil
	}

	// 3. Get the cached list of remote resources.
	remoteList, err := cache.list(ctx, desc)
	if err != nil {
		return nil, false, err
	}
	if remoteList == nil {
		return nil, false, nil
	}

	// 4. Native identities must be unique; existing adapter first-match semantics stay unchanged.
	matches := 0
	for i := range remoteList.Items {
		remoteKey, ok := extractor(&remoteList.Items[i])
		if ok && remoteKey == localKey {
			if !nativePolicy {
				return remoteList.Items[i].DeepCopy(), true, nil
			}
			matches++
			matched = remoteList.Items[i].DeepCopy()
		}
	}

	if matches > 1 {
		return nil, false, fmt.Errorf("ambiguous native identity %q: %d matching resources", localKey, matches)
	}
	return matched, matches == 1, nil
}
