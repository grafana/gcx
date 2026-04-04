package remote_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestMockPushClient_SatisfiesInterfaces(t *testing.T) {
	var client any = &mockPushClient{}
	_, ok := client.(remote.PushClient)
	require.True(t, ok, "mockPushClient must satisfy PushClient")
	_, ok = client.(remote.PushLister)
	require.True(t, ok, "mockPushClient must satisfy PushLister")
}

func TestPusher_Push_FoldersFirst(t *testing.T) {
	req := require.New(t)

	// Create test resources: 2 folders and 2 dashboards
	testResources := createTestResources()

	// Mock client that records the order of operations
	mockClient := &mockPushClient{
		operations: []string{},
		mu:         sync.Mutex{},
	}

	// Mock registry that supports all test resources
	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{
			{
				GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1"},
				Kind:         "Folder",
				Singular:     "folder",
				Plural:       "folders",
			},
			{
				GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
				Kind:         "Dashboard",
				Singular:     "dashboard",
				Plural:       "dashboards",
			},
		},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	// Push resources
	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 2,
		IncludeManaged: true, // Include managed resources
	})

	req.NoError(err)
	req.Equal(4, summary.SuccessCount())
	req.Equal(0, summary.FailedCount())

	// Verify that all folders were pushed before all dashboards
	req.Len(mockClient.operations, 4)

	// Extract resource names and kinds from operations
	var folderOps, dashboardOps []string
	for _, op := range mockClient.operations {
		if contains(op, "folder-1") || contains(op, "folder-2") {
			folderOps = append(folderOps, op)
		} else if contains(op, "dashboard-1") || contains(op, "dashboard-2") {
			dashboardOps = append(dashboardOps, op)
		}
	}

	req.Len(folderOps, 2, "Should have 2 folder operations")
	req.Len(dashboardOps, 2, "Should have 2 dashboard operations")

	// Find the index of the last folder operation and first dashboard operation
	lastFolderIndex := -1
	firstDashboardIndex := len(mockClient.operations)

	for i, op := range mockClient.operations {
		if contains(op, "folder-1") || contains(op, "folder-2") {
			if i > lastFolderIndex {
				lastFolderIndex = i
			}
		}
		if (contains(op, "dashboard-1") || contains(op, "dashboard-2")) && i < firstDashboardIndex {
			firstDashboardIndex = i
		}
	}

	req.Less(lastFolderIndex, firstDashboardIndex,
		"All folders should be pushed before any dashboard. Last folder at index %d, first dashboard at index %d",
		lastFolderIndex, firstDashboardIndex)
}

func TestPusher_Push_OnlyFolders(t *testing.T) {
	req := require.New(t)

	// Create resources with only folders
	testResources := resources.NewResources(
		createFolderResource("folder-1", "v1"),
		createFolderResource("folder-2", "v0alpha1"),
	)

	mockClient := &mockPushClient{
		operations: []string{},
		mu:         sync.Mutex{},
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{
			{
				GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1"},
				Kind:         "Folder",
				Singular:     "folder",
				Plural:       "folders",
			},
			{
				GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v0alpha1"},
				Kind:         "Folder",
				Singular:     "folder",
				Plural:       "folders",
			},
		},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 2,
		IncludeManaged: true, // Include managed resources
	})

	req.NoError(err)
	req.Equal(2, summary.SuccessCount())
	req.Equal(0, summary.FailedCount())
	req.Len(mockClient.operations, 2)
}

func TestPusher_Push_OnlyDashboards(t *testing.T) {
	req := require.New(t)

	// Create resources with only dashboards
	testResources := resources.NewResources(
		createDashboardResource("dashboard-1"),
		createDashboardResource("dashboard-2"),
	)

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

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 2,
		IncludeManaged: true, // Include managed resources
	})

	req.NoError(err)
	req.Equal(2, summary.SuccessCount())
	req.Equal(0, summary.FailedCount())
	req.Len(mockClient.operations, 2)
}

func TestPusher_Push_EmptyResources(t *testing.T) {
	req := require.New(t)

	testResources := resources.NewResources()

	mockClient := &mockPushClient{
		operations: []string{},
		mu:         sync.Mutex{},
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 2,
	})

	req.NoError(err)
	req.Equal(0, summary.SuccessCount())
	req.Equal(0, summary.FailedCount())
	req.Empty(mockClient.operations)
}

func TestPusher_Push_FolderCreationError(t *testing.T) {
	req := require.New(t)

	testResources := createTestResources()

	mockClient := &mockPushClient{
		operations:   []string{},
		mu:           sync.Mutex{},
		shouldFail:   map[string]bool{"folder-1": true},
		failureError: errors.New("folder creation failed"),
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{
			{
				GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1"},
				Kind:         "Folder",
				Singular:     "folder",
				Plural:       "folders",
			},
			{
				GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
				Kind:         "Dashboard",
				Singular:     "dashboard",
				Plural:       "dashboards",
			},
		},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 2,
		StopOnError:    false,
		IncludeManaged: true, // Include managed resources
	})

	req.NoError(err)
	req.Equal(3, summary.SuccessCount()) // 1 folder + 2 dashboards succeeded
	req.Equal(1, summary.FailedCount())  // 1 folder failed
	req.Len(summary.Failures(), 1)
	req.Equal("folder-1", summary.Failures()[0].Resource.Name())
}

func TestPusher_Push_NestedFolders(t *testing.T) {
	req := require.New(t)

	// Create a 3-level folder hierarchy:
	// root-folder (no parent)
	//   └─ child-folder-1 (parent: root-folder)
	//        └─ grandchild-folder (parent: child-folder-1)
	//   └─ child-folder-2 (parent: root-folder)
	testResources := resources.NewResources(
		createFolderWithParent("root-folder", ""),
		createFolderWithParent("child-folder-1", "root-folder"),
		createFolderWithParent("child-folder-2", "root-folder"),
		createFolderWithParent("grandchild-folder", "child-folder-1"),
	)

	mockClient := &mockPushClient{
		operations: []string{},
		mu:         sync.Mutex{},
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{
			{
				GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1"},
				Kind:         "Folder",
				Singular:     "folder",
				Plural:       "folders",
			},
		},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 2,
		IncludeManaged: true,
	})

	req.NoError(err)
	req.Equal(4, summary.SuccessCount())
	req.Equal(0, summary.FailedCount())
	req.Len(mockClient.operations, 4)

	// Verify that parent folders are pushed before child folders
	// Build a position map
	positions := make(map[string]int)
	for i, op := range mockClient.operations {
		switch {
		case contains(op, "root-folder"):
			positions["root-folder"] = i
		case contains(op, "child-folder-1"):
			positions["child-folder-1"] = i
		case contains(op, "child-folder-2"):
			positions["child-folder-2"] = i
		case contains(op, "grandchild-folder"):
			positions["grandchild-folder"] = i
		}
	}

	// Verify ordering: parent must come before children
	req.Less(positions["root-folder"], positions["child-folder-1"],
		"root-folder must be pushed before child-folder-1")
	req.Less(positions["root-folder"], positions["child-folder-2"],
		"root-folder must be pushed before child-folder-2")
	req.Less(positions["child-folder-1"], positions["grandchild-folder"],
		"child-folder-1 must be pushed before grandchild-folder")
}

func TestPusher_Push_MultipleFolderTrees(t *testing.T) {
	req := require.New(t)

	// Create two independent folder trees:
	// tree-a-root
	//   └─ tree-a-child
	// tree-b-root
	//   └─ tree-b-child
	testResources := resources.NewResources(
		createFolderWithParent("tree-a-root", ""),
		createFolderWithParent("tree-a-child", "tree-a-root"),
		createFolderWithParent("tree-b-root", ""),
		createFolderWithParent("tree-b-child", "tree-b-root"),
	)

	mockClient := &mockPushClient{
		operations: []string{},
		mu:         sync.Mutex{},
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{
			{
				GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1"},
				Kind:         "Folder",
				Singular:     "folder",
				Plural:       "folders",
			},
		},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 2,
		IncludeManaged: true,
	})

	req.NoError(err)
	req.Equal(4, summary.SuccessCount())
	req.Equal(0, summary.FailedCount())
	req.Len(mockClient.operations, 4)

	// Verify ordering within each tree
	positions := make(map[string]int)
	for i, op := range mockClient.operations {
		switch {
		case contains(op, "tree-a-root"):
			positions["tree-a-root"] = i
		case contains(op, "tree-a-child"):
			positions["tree-a-child"] = i
		case contains(op, "tree-b-root"):
			positions["tree-b-root"] = i
		case contains(op, "tree-b-child"):
			positions["tree-b-child"] = i
		}
	}

	// Verify ordering: parent must come before children in each tree
	req.Less(positions["tree-a-root"], positions["tree-a-child"],
		"tree-a-root must be pushed before tree-a-child")
	req.Less(positions["tree-b-root"], positions["tree-b-child"],
		"tree-b-root must be pushed before tree-b-child")
}

func TestPusher_Push_OrphanedFolder(t *testing.T) {
	req := require.New(t)

	// Create a folder that references a non-existent parent
	testResources := resources.NewResources(
		createFolderWithParent("orphan-folder", "non-existent-parent"),
	)

	mockClient := &mockPushClient{
		operations: []string{},
		mu:         sync.Mutex{},
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{
			{
				GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1"},
				Kind:         "Folder",
				Singular:     "folder",
				Plural:       "folders",
			},
		},
	}

	pusher := remote.NewPusher(mockClient, mockRegistry)

	// Orphaned folders should still be pushed (treated as root folders)
	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 2,
		IncludeManaged: true,
	})

	req.NoError(err)
	req.Equal(1, summary.SuccessCount())
	req.Equal(0, summary.FailedCount())
	req.Len(mockClient.operations, 1)
}

// Helper functions

func createTestResources() *resources.Resources {
	return resources.NewResources(
		createFolderResource("folder-1", "v1"),
		createFolderResource("folder-2", "v1"),
		createDashboardResource("dashboard-1"),
		createDashboardResource("dashboard-2"),
	)
}

func createFolderResource(name, version string) *resources.Resource {
	return resources.MustFromObject(map[string]any{
		"apiVersion": "folder.grafana.app/" + version,
		"kind":       "Folder",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "default",
		},
		"spec": map[string]any{
			"title": "Test Folder " + name,
		},
	}, resources.SourceInfo{})
}

func createFolderWithParent(name, parentUID string) *resources.Resource {
	metadata := map[string]any{
		"name":      name,
		"namespace": "default",
	}

	// Add parent annotation if parentUID is not empty
	if parentUID != "" {
		metadata["annotations"] = map[string]any{
			"grafana.app/folder": parentUID,
		}
	}

	return resources.MustFromObject(map[string]any{
		"apiVersion": "folder.grafana.app/v1",
		"kind":       "Folder",
		"metadata":   metadata,
		"spec": map[string]any{
			"title": "Test Folder " + name,
		},
	}, resources.SourceInfo{})
}

func createDashboardResource(name string) *resources.Resource {
	return resources.MustFromObject(map[string]any{
		"apiVersion": "dashboard.grafana.app/v1",
		"kind":       "Dashboard",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "default",
		},
		"spec": map[string]any{
			"title": "Test Dashboard " + name,
		},
	}, resources.SourceInfo{})
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestPusher_Push_UpdateExistingResource(t *testing.T) {
	tests := []struct {
		name                 string
		localResources       []*resources.Resource
		existingResources    map[string]*unstructured.Unstructured
		wantOperations       []string
		wantResourceVersions map[string]string
		wantPushedCount      int
		wantFailedCount      int
	}{
		{
			name: "update single dashboard with resourceVersion from server",
			localResources: []*resources.Resource{
				createDashboardResource("dashboard-1"),
			},
			existingResources: map[string]*unstructured.Unstructured{
				"dashboard-1": makeExistingDashboard("dashboard-1", "42"),
			},
			wantOperations:       []string{"update-dashboard-1"},
			wantResourceVersions: map[string]string{"dashboard-1": "42"},
			wantPushedCount:      1,
			wantFailedCount:      0,
		},
		{
			name: "mix of create and update operations",
			localResources: []*resources.Resource{
				createDashboardResource("dashboard-new"),
				createDashboardResource("dashboard-existing"),
			},
			existingResources: map[string]*unstructured.Unstructured{
				"dashboard-existing": makeExistingDashboard("dashboard-existing", "99"),
			},
			wantOperations:       []string{"create-dashboard-new", "update-dashboard-existing"},
			wantResourceVersions: map[string]string{"dashboard-existing": "99"},
			wantPushedCount:      2,
			wantFailedCount:      0,
		},
		{
			name: "update folder with resourceVersion",
			localResources: []*resources.Resource{
				createFolderResource("folder-1", "v1"),
			},
			existingResources: map[string]*unstructured.Unstructured{
				"folder-1": makeExistingFolder("folder-1", "7"),
			},
			wantOperations:       []string{"update-folder-1"},
			wantResourceVersions: map[string]string{"folder-1": "7"},
			wantPushedCount:      1,
			wantFailedCount:      0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)

			mockClient := &mockPushClient{
				operations:        []string{},
				mu:                sync.Mutex{},
				existingResources: tc.existingResources,
			}

			mockRegistry := &mockPushRegistry{
				supportedResources: []resources.Descriptor{
					{
						GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1"},
						Kind:         "Folder",
						Singular:     "folder",
						Plural:       "folders",
					},
					{
						GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
						Kind:         "Dashboard",
						Singular:     "dashboard",
						Plural:       "dashboards",
					},
				},
			}

			pusher := remote.NewPusher(mockClient, mockRegistry)
			testResources := resources.NewResources(tc.localResources...)

			summary, err := pusher.Push(t.Context(), remote.PushRequest{
				Resources:      testResources,
				MaxConcurrency: 1,
				IncludeManaged: true,
			})

			req.NoError(err)
			req.Equal(tc.wantPushedCount, summary.SuccessCount())
			req.Equal(tc.wantFailedCount, summary.FailedCount())
			req.ElementsMatch(tc.wantOperations, mockClient.operations)

			for name, expectedRV := range tc.wantResourceVersions {
				updated, ok := mockClient.updatedObjects[name]
				req.True(ok, "expected object %s to be updated", name)
				req.Equal(expectedRV, updated.GetResourceVersion(),
					"resourceVersion mismatch for %s", name)
			}
		})
	}
}

func TestPusher_Push_UpdateFailure(t *testing.T) {
	req := require.New(t)

	mockClient := &mockPushClient{
		operations: []string{},
		mu:         sync.Mutex{},
		existingResources: map[string]*unstructured.Unstructured{
			"dashboard-1": makeExistingDashboard("dashboard-1", "42"),
		},
		shouldFail:   map[string]bool{"dashboard-1": true},
		failureError: errors.New("update failed"),
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
	testResources := resources.NewResources(
		createDashboardResource("dashboard-1"),
	)

	summary, err := pusher.Push(t.Context(), remote.PushRequest{
		Resources:      testResources,
		MaxConcurrency: 1,
		IncludeManaged: true,
	})

	req.NoError(err)
	req.Equal(0, summary.SuccessCount())
	req.Equal(1, summary.FailedCount())
	req.Len(summary.Failures(), 1)
	req.Equal("dashboard-1", summary.Failures()[0].Resource.Name())
}

func makeExistingDashboard(name, resourceVersion string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "dashboard.grafana.app/v1",
			"kind":       "Dashboard",
			"metadata": map[string]any{
				"name":            name,
				"namespace":       "default",
				"resourceVersion": resourceVersion,
			},
			"spec": map[string]any{
				"title": "Existing Dashboard " + name,
			},
		},
	}
}

func makeExistingFolder(name, resourceVersion string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "folder.grafana.app/v1",
			"kind":       "Folder",
			"metadata": map[string]any{
				"name":            name,
				"namespace":       "default",
				"resourceVersion": resourceVersion,
			},
			"spec": map[string]any{
				"title": "Existing Folder " + name,
			},
		},
	}
}

// Mock implementations

type mockPushClient struct {
	operations        []string
	mu                sync.Mutex
	shouldFail        map[string]bool
	failureError      error
	existingResources map[string]*unstructured.Unstructured
	updatedObjects    map[string]*unstructured.Unstructured
	listResults       map[schema.GroupVersionKind]*unstructured.UnstructuredList
	listError         error
}

func (m *mockPushClient) Create(
	_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.CreateOptions,
) (*unstructured.Unstructured, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := obj.GetName()
	m.operations = append(m.operations, "create-"+name)

	if m.shouldFail != nil && m.shouldFail[name] {
		return nil, m.failureError
	}

	return obj, nil
}

func (m *mockPushClient) Update(
	_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions,
) (*unstructured.Unstructured, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := obj.GetName()
	m.operations = append(m.operations, "update-"+name)

	if m.updatedObjects == nil {
		m.updatedObjects = make(map[string]*unstructured.Unstructured)
	}
	m.updatedObjects[name] = obj.DeepCopy()

	if m.shouldFail != nil && m.shouldFail[name] {
		return nil, m.failureError
	}

	return obj, nil
}

func (m *mockPushClient) Get(
	_ context.Context, desc resources.Descriptor, name string, _ metav1.GetOptions,
) (*unstructured.Unstructured, error) {
	if m.existingResources != nil {
		if existing, ok := m.existingResources[name]; ok {
			return existing, nil
		}
	}

	// Simulate resource not found to trigger Create operations.
	return nil, apierrors.NewNotFound(desc.GroupVersionResource().GroupResource(), name)
}

func (m *mockPushClient) List(
	_ context.Context, desc resources.Descriptor, _ metav1.ListOptions,
) (*unstructured.UnstructuredList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.listError != nil {
		return nil, m.listError
	}

	if m.listResults != nil {
		if result, ok := m.listResults[desc.GroupVersionKind()]; ok {
			return result, nil
		}
	}
	return &unstructured.UnstructuredList{}, nil
}

type mockPushRegistry struct {
	supportedResources []resources.Descriptor
}

func (m *mockPushRegistry) SupportedResources() resources.Descriptors {
	return m.supportedResources
}
