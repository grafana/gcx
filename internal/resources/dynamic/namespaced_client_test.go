package dynamic_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// testDescriptor creates a Descriptor for the dashboard resource type used in tests.
func testDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
		Kind:         "Dashboard",
		Singular:     "dashboard",
		Plural:       "dashboards",
	}
}

// testDashboard creates a minimal unstructured dashboard object for testing.
func testDashboard(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "dashboard.grafana.app/v1",
			"kind":       "Dashboard",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "default",
			},
			"spec": map[string]any{
				"title": "Test Dashboard " + name,
			},
		},
	}
}

// newFakeClientWithPagination creates a fake dynamic client with a custom reactor
// that simulates server-side pagination. The standard fake ObjectTracker does not
// honour Limit/Continue, so we intercept list calls and paginate manually.
func newFakeClientWithPagination(objects []*unstructured.Unstructured) *fake.FakeDynamicClient {
	scheme := runtime.NewScheme()

	gvk := testDescriptor().GroupVersionKind()
	listGVK := gvk.GroupVersion().WithKind(gvk.Kind + "List")
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})

	runtimeObjects := make([]runtime.Object, len(objects))
	for i, obj := range objects {
		runtimeObjects[i] = obj.DeepCopy()
	}

	client := fake.NewSimpleDynamicClient(scheme, runtimeObjects...)

	// Prepend a reactor that simulates pagination. When Limit > 0 the reactor
	// returns at most Limit items and sets a Continue token when more items
	// exist. When Limit == 0 it returns every item with no Continue token.
	client.PrependReactor("list", "dashboards", func(action k8stesting.Action) (bool, runtime.Object, error) {
		listAction, ok := action.(k8stesting.ListActionImpl)
		if !ok {
			return false, nil, nil
		}

		limit := listAction.ListOptions.Limit
		continueToken := listAction.ListOptions.Continue

		// Build the full item list filtered by namespace.
		allItems := make([]unstructured.Unstructured, 0, len(objects))
		for _, obj := range objects {
			ns := listAction.GetNamespace()
			if ns == "" || obj.GetNamespace() == ns {
				allItems = append(allItems, *obj.DeepCopy())
			}
		}

		// Decode start index from continue token.
		startIdx := 0
		if continueToken != "" {
			if _, err := fmt.Sscanf(continueToken, "%d", &startIdx); err != nil {
				return true, nil, fmt.Errorf("invalid continue token: %s", continueToken)
			}
		}

		// No limit: return everything from startIdx.
		if limit == 0 {
			result := &unstructured.UnstructuredList{
				Object: map[string]any{
					"apiVersion": gvk.GroupVersion().String(),
					"kind":       listGVK.Kind,
				},
			}
			if startIdx < len(allItems) {
				result.Items = allItems[startIdx:]
			}
			return true, result, nil
		}

		// Apply limit.
		endIdx := min(startIdx+int(limit), len(allItems))

		result := &unstructured.UnstructuredList{
			Object: map[string]any{
				"apiVersion": gvk.GroupVersion().String(),
				"kind":       listGVK.Kind,
			},
		}
		if startIdx < len(allItems) {
			result.Items = allItems[startIdx:endIdx]
		}

		// Set continue token when more items remain.
		if endIdx < len(allItems) {
			result.SetContinue(strconv.Itoa(endIdx))
		}

		return true, result, nil
	})

	return client
}

func TestNamespacedClient_List(t *testing.T) {
	tests := []struct {
		name            string
		objectCount     int
		limit           int64
		wantItemCount   int
		wantHasContinue bool
	}{
		{
			name:            "list with limit returns limited items and preserves continue token",
			objectCount:     10,
			limit:           3,
			wantItemCount:   3,
			wantHasContinue: true,
		},
		{
			name:            "list without limit returns all items",
			objectCount:     5,
			limit:           0,
			wantItemCount:   5,
			wantHasContinue: false,
		},
		{
			name:            "list with limit larger than total returns all items",
			objectCount:     3,
			limit:           10,
			wantItemCount:   3,
			wantHasContinue: false,
		},
		{
			name:            "list with limit=1 returns single item",
			objectCount:     5,
			limit:           1,
			wantItemCount:   1,
			wantHasContinue: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)

			objects := make([]*unstructured.Unstructured, tc.objectCount)
			for i := range tc.objectCount {
				objects[i] = testDashboard(fmt.Sprintf("dash-%d", i))
			}

			fakeClient := newFakeClientWithPagination(objects)
			client := dynamic.NewNamespacedClient("default", fakeClient)

			desc := testDescriptor()
			opts := metav1.ListOptions{Limit: tc.limit}

			result, err := client.List(context.Background(), desc, opts)
			req.NoError(err)
			req.NotNil(result)
			req.Len(result.Items, tc.wantItemCount)

			hasContinue := result.GetContinue() != ""
			req.Equal(tc.wantHasContinue, hasContinue,
				"expected continue token present=%v, got continue=%q",
				tc.wantHasContinue, result.GetContinue())
		})
	}
}
