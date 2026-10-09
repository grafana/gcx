package remote

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// findByNaturalKey attempts to find an existing remote resource that matches
// the local resource by content-based natural key. Returns the remote resource's
// name and resourceVersion if found.
//
// This enables cross-stack push: when Get-by-metadata.name returns NotFound
// (because the server-generated ID from Stack A doesn't exist on Stack B),
// we fall back to matching by natural key (e.g., SLO name, check job+target).
func findByNaturalKey( //nolint:nonamedreturns // Named returns document the four-value return contract.
	ctx context.Context,
	cache *naturalKeyCache,
	desc resources.Descriptor,
	localObj *unstructured.Unstructured,
) (remoteName string, resourceVersion string, found bool, err error) {
	// 1. Get the extractor for this GVK.
	extractor := adapter.GetNaturalKeyExtractor(desc.GroupVersionKind())
	nativePolicy := extractor == nil && resources.HasServerDerivedName(desc.GroupVersionKind())
	if extractor == nil && !nativePolicy {
		return "", "", false, nil
	}
	var identityErr error
	if nativePolicy {
		extractor = func(obj *unstructured.Unstructured) (string, bool) {
			key, _, err := resources.NativeIdentity(obj)
			if err != nil {
				identityErr = err
				return "", false
			}
			return key, true
		}
	}

	// 2. Extract the local resource's natural key.
	localKey, ok := extractor(localObj)
	if !ok {
		return "", "", false, identityErr
	}

	// 3. Get the cached list of remote resources.
	remoteList, err := cache.list(ctx, desc)
	if err != nil {
		return "", "", false, err
	}
	if remoteList == nil {
		return "", "", false, nil
	}

	// 4. Native identities must be unique; existing adapter first-match semantics stay unchanged.
	matches := 0
	for i := range remoteList.Items {
		remoteKey, ok := extractor(&remoteList.Items[i])
		if ok && remoteKey == localKey {
			if !nativePolicy {
				return remoteList.Items[i].GetName(), remoteList.Items[i].GetResourceVersion(), true, nil
			}
			matches++
			remoteName = remoteList.Items[i].GetName()
			resourceVersion = remoteList.Items[i].GetResourceVersion()
		}
	}

	if nativePolicy && identityErr != nil {
		return "", "", false, identityErr
	}
	if matches > 1 {
		return "", "", false, fmt.Errorf("ambiguous native identity %q: %d matching resources", localKey, matches)
	}
	return remoteName, resourceVersion, matches == 1, nil
}
