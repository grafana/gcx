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
// the local resource by content-based natural key. Returns the matching remote
// resource, including its manager fields and destination provenance, if found.
//
// This enables cross-stack push: when Get-by-metadata.name returns NotFound
// (because the server-generated ID from Stack A doesn't exist on Stack B),
// we fall back to matching by natural key (e.g., SLO name, check job+target).
func findByNaturalKey( //nolint:nonamedreturns // Named returns document the match contract.
	ctx context.Context,
	cache *naturalKeyCache,
	desc resources.Descriptor,
	localObj *unstructured.Unstructured,
	desiredNativeIdentity string,
) (matched *unstructured.Unstructured, found bool, err error) {
	// 1. Get the extractor for this GVK.
	extractor := adapter.GetNaturalKeyExtractor(desc.GroupVersionKind())
	nativePolicy := extractor == nil && resources.HasServerDerivedName(desc.GroupVersionKind())
	if extractor == nil && !nativePolicy {
		return nil, false, nil
	}
	if nativePolicy {
		// The pusher validates local manifests; unreadable server items are skipped.
		extractor = func(obj *unstructured.Unstructured) (string, bool) {
			key, _, err := resources.NativeIdentity(obj)
			if err != nil {
				logging.FromContext(ctx).Debug("Skipping remote resource with invalid native identity", "name", obj.GetName(), logs.Err(err))
				return "", false
			}
			return key, true
		}
	}

	// 2. Native identity was validated and extracted by the pusher; adapter
	// identities still use their own extractor.
	localKey := desiredNativeIdentity
	if !nativePolicy {
		var ok bool
		localKey, ok = extractor(localObj)
		if !ok {
			return nil, false, nil
		}
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
