package remote_test

import (
	"context"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Each test uses a unique GVK group to avoid collisions since we cannot
// reset the global natural key registry from outside the adapter package.

func makeTestGVK(group string) schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: group, Version: "v1", Kind: "TestResource"}
}

func makeTestDescriptor(group string) resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: group, Version: "v1"},
		Kind:         "TestResource",
		Singular:     "testresource",
		Plural:       "testresources",
	}
}

func makeTestResource(group, name, specName string) *resources.Resource {
	return resources.MustFromObject(map[string]any{
		"apiVersion": group + "/v1",
		"kind":       "TestResource",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "default",
		},
		"spec": map[string]any{
			"name": specName,
		},
	}, resources.SourceInfo{})
}

func makeTestUnstructured(group, name, specName, resourceVersion string) unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": group + "/v1",
		"kind":       "TestResource",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "default",
		},
		"spec": map[string]any{
			"name": specName,
		},
	}
	if resourceVersion != "" {
		meta, _ := obj["metadata"].(map[string]any)
		meta["resourceVersion"] = resourceVersion
	}
	return unstructured.Unstructured{Object: obj}
}

// TestPusher_CrossStackUpdate_NaturalKey verifies that when Get returns NotFound
// and a natural key extractor is registered, the pusher finds the remote resource
// by natural key and performs an Update instead of Create.
func TestPusher_CrossStackUpdate_NaturalKey(t *testing.T) {
	const group = "crossstack.test.grafana.app"
	gvk := makeTestGVK(group)
	adapter.RegisterNaturalKey(gvk, adapter.SpecFieldKey("name"))

	req := require.New(t)

	// Remote has a resource with a different metadata.name but the same spec.name.
	remoteObj := makeTestUnstructured(group, "remote-uuid", "My Resource", "55")

	mockClient := &mockPushClient{
		operations: []string{},
		mu:         sync.Mutex{},
		// Get will return NotFound for "local-uuid"
		existingResources: nil,
		// List returns the remote resource
		listResults: map[schema.GroupVersionKind]*unstructured.UnstructuredList{
			gvk: {Items: []unstructured.Unstructured{remoteObj}},
		},
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{makeTestDescriptor(group)},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	localRes := makeTestResource(group, "local-uuid", "My Resource")
	testResources := resources.NewResources(localRes)

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 1,
		IncludeManaged: true,
	})

	req.NoError(err)
	req.Equal(1, summary.SuccessCount())
	req.Equal(0, summary.FailedCount())

	// Should have performed an update (not create) with the remote name.
	req.Len(mockClient.operations, 1)
	req.Equal("update-remote-uuid", mockClient.operations[0])

	// Verify the updated object has the remote name and resource version.
	updated, ok := mockClient.updatedObjects["remote-uuid"]
	req.True(ok, "expected update with remote name")
	req.Equal("55", updated.GetResourceVersion())
}

// TestPusher_NoNaturalKey_FallsBackToCreate verifies that when no natural key
// extractor is registered, the pusher falls back to Create on NotFound.
func TestPusher_NoNaturalKey_FallsBackToCreate(t *testing.T) {
	// dashboards have no natural key registered by default
	req := require.New(t)

	mockClient := &mockPushClient{
		operations: []string{},
		mu:         sync.Mutex{},
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{
			{
				GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
				Kind:         "Dashboard",
				Singular:     "dashboard",
				Plural:       "dashboards",
			},
		},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)
	testResources := resources.NewResources(createDashboardResource("new-dash"))

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 1,
		IncludeManaged: true,
	})

	req.NoError(err)
	req.Equal(1, summary.SuccessCount())
	req.Len(mockClient.operations, 1)
	req.Equal("create-new-dash", mockClient.operations[0])
}

// TestPusher_SameStackUpdate_SkipsNaturalKey verifies that when Get succeeds
// (same-stack update), the natural key path is never exercised and List is never called.
func TestPusher_SameStackUpdate_SkipsNaturalKey(t *testing.T) {
	const group = "samestack.test.grafana.app"
	gvk := makeTestGVK(group)
	adapter.RegisterNaturalKey(gvk, adapter.SpecFieldKey("name"))

	req := require.New(t)

	existingObj := makeTestUnstructured(group, "my-id", "My Resource", "10")

	mockClient := &listTrackingMockClient{
		mockPushClient: mockPushClient{
			operations: []string{},
			mu:         sync.Mutex{},
			existingResources: map[string]*unstructured.Unstructured{
				"my-id": &existingObj,
			},
		},
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{makeTestDescriptor(group)},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	localRes := makeTestResource(group, "my-id", "My Resource")
	testResources := resources.NewResources(localRes)

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 1,
		IncludeManaged: true,
	})

	req.NoError(err)
	req.Equal(1, summary.SuccessCount())
	req.Len(mockClient.operations, 1)
	req.Equal("update-my-id", mockClient.operations[0])
	req.Equal(0, mockClient.listCallCount, "List should not be called when Get succeeds")
}

// TestPusher_NaturalKey_NoMatchInRemote verifies that when the natural key
// doesn't match any remote resource, the pusher falls back to Create.
func TestPusher_NaturalKey_NoMatchInRemote(t *testing.T) {
	const group = "nomatch.test.grafana.app"
	gvk := makeTestGVK(group)
	adapter.RegisterNaturalKey(gvk, adapter.SpecFieldKey("name"))

	req := require.New(t)

	// Remote has a resource with a different spec.name.
	remoteObj := makeTestUnstructured(group, "remote-uuid", "Different Resource", "22")

	mockClient := &mockPushClient{
		operations:        []string{},
		mu:                sync.Mutex{},
		existingResources: nil,
		listResults: map[schema.GroupVersionKind]*unstructured.UnstructuredList{
			gvk: {Items: []unstructured.Unstructured{remoteObj}},
		},
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{makeTestDescriptor(group)},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	localRes := makeTestResource(group, "local-uuid", "My Resource")
	testResources := resources.NewResources(localRes)

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 1,
		IncludeManaged: true,
	})

	req.NoError(err)
	req.Equal(1, summary.SuccessCount())
	req.Len(mockClient.operations, 1)
	req.Equal("create-local-uuid", mockClient.operations[0])
}

// TestPusher_NaturalKey_EmptyRemoteList verifies that when the remote list
// is empty, the pusher falls back to Create.
func TestPusher_NaturalKey_EmptyRemoteList(t *testing.T) {
	const group = "emptylist.test.grafana.app"
	gvk := makeTestGVK(group)
	adapter.RegisterNaturalKey(gvk, adapter.SpecFieldKey("name"))

	req := require.New(t)

	mockClient := &mockPushClient{
		operations:        []string{},
		mu:                sync.Mutex{},
		existingResources: nil,
		listResults: map[schema.GroupVersionKind]*unstructured.UnstructuredList{
			gvk: {Items: []unstructured.Unstructured{}},
		},
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{makeTestDescriptor(group)},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	localRes := makeTestResource(group, "local-uuid", "My Resource")
	testResources := resources.NewResources(localRes)

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 1,
		IncludeManaged: true,
	})

	req.NoError(err)
	req.Equal(1, summary.SuccessCount())
	req.Len(mockClient.operations, 1)
	req.Equal("create-local-uuid", mockClient.operations[0])
}

// TestPusher_NaturalKey_MultipleRemoteOneMatch verifies that when multiple remote
// resources exist, the correct one is matched by natural key.
func TestPusher_NaturalKey_MultipleRemoteOneMatch(t *testing.T) {
	const group = "multimatch.test.grafana.app"
	gvk := makeTestGVK(group)
	adapter.RegisterNaturalKey(gvk, adapter.SpecFieldKey("name"))

	req := require.New(t)

	remoteObjs := []unstructured.Unstructured{
		makeTestUnstructured(group, "remote-1", "First Resource", "10"),
		makeTestUnstructured(group, "remote-2", "Target Resource", "20"),
		makeTestUnstructured(group, "remote-3", "Third Resource", "30"),
	}

	mockClient := &mockPushClient{
		operations:        []string{},
		mu:                sync.Mutex{},
		existingResources: nil,
		listResults: map[schema.GroupVersionKind]*unstructured.UnstructuredList{
			gvk: {Items: remoteObjs},
		},
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{makeTestDescriptor(group)},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	localRes := makeTestResource(group, "local-uuid", "Target Resource")
	testResources := resources.NewResources(localRes)

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 1,
		IncludeManaged: true,
	})

	req.NoError(err)
	req.Equal(1, summary.SuccessCount())
	req.Len(mockClient.operations, 1)
	req.Equal("update-remote-2", mockClient.operations[0])

	updated, ok := mockClient.updatedObjects["remote-2"]
	req.True(ok, "expected update with remote-2 name")
	req.Equal("20", updated.GetResourceVersion())
}

// listTrackingMockClient wraps mockPushClient and tracks List calls.
type listTrackingMockClient struct {
	mockPushClient

	listCallCount int
}

func (m *listTrackingMockClient) List(
	ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions,
) (*unstructured.UnstructuredList, error) {
	m.mu.Lock()
	m.listCallCount++
	m.mu.Unlock()
	return m.mockPushClient.List(ctx, desc, opts)
}
