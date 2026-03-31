package remote_test

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// mockDeleteClient implements remote.DeleteClient for testing.
type mockDeleteClient struct {
	shouldFail   map[string]bool
	failureError error
	deletedNames []string
}

func (m *mockDeleteClient) Delete(
	_ context.Context, _ resources.Descriptor, name string, _ metav1.DeleteOptions,
) error {
	if m.shouldFail != nil && m.shouldFail[name] {
		return m.failureError
	}

	m.deletedNames = append(m.deletedNames, name)
	return nil
}

func TestDeleter_Delete(t *testing.T) {
	dashboardDescriptor := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
		Kind:         "Dashboard",
		Singular:     "dashboard",
		Plural:       "dashboards",
	}
	folderDescriptor := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1"},
		Kind:         "Folder",
		Singular:     "folder",
		Plural:       "folders",
	}

	tests := []struct {
		name               string
		resources          []*resources.Resource
		supportedResources []resources.Descriptor
		clientShouldFail   map[string]bool
		clientFailureError error
		stopOnError        bool
		wantSuccessCount   int
		wantFailedCount    int
		wantErr            bool
		wantDeletedNames   []string
	}{
		{
			name: "delete single dashboard successfully",
			resources: []*resources.Resource{
				createDashboardResource("dashboard-1"),
			},
			supportedResources: []resources.Descriptor{dashboardDescriptor},
			wantSuccessCount:   1,
			wantFailedCount:    0,
			wantDeletedNames:   []string{"dashboard-1"},
		},
		{
			name: "delete multiple resources successfully",
			resources: []*resources.Resource{
				createDashboardResource("dashboard-1"),
				createDashboardResource("dashboard-2"),
				createFolderResource("folder-1", "v1"),
			},
			supportedResources: []resources.Descriptor{dashboardDescriptor, folderDescriptor},
			wantSuccessCount:   3,
			wantFailedCount:    0,
		},
		{
			name:               "delete empty resource list",
			resources:          []*resources.Resource{},
			supportedResources: []resources.Descriptor{dashboardDescriptor},
			wantSuccessCount:   0,
			wantFailedCount:    0,
			wantDeletedNames:   nil,
		},
		{
			name: "skip resource with unsupported GVK",
			resources: []*resources.Resource{
				createDashboardResource("dashboard-1"),
			},
			supportedResources: []resources.Descriptor{}, // no supported resources
			wantSuccessCount:   0,
			wantFailedCount:    0, // unsupported is skipped, not counted as failure
		},
		{
			name: "record failure when delete call fails",
			resources: []*resources.Resource{
				createDashboardResource("dashboard-1"),
				createDashboardResource("dashboard-2"),
			},
			supportedResources: []resources.Descriptor{dashboardDescriptor},
			clientShouldFail:   map[string]bool{"dashboard-1": true},
			clientFailureError: errors.New("delete API error"),
			wantSuccessCount:   1,
			wantFailedCount:    1,
		},
		{
			name: "stop on error when StopOnError is set",
			resources: []*resources.Resource{
				createDashboardResource("dashboard-fail"),
			},
			supportedResources: []resources.Descriptor{dashboardDescriptor},
			clientShouldFail:   map[string]bool{"dashboard-fail": true},
			clientFailureError: errors.New("delete failed"),
			stopOnError:        true,
			wantErr:            true,
			wantFailedCount:    1,
		},
		{
			name: "all resources fail without stop on error",
			resources: []*resources.Resource{
				createDashboardResource("dashboard-1"),
				createDashboardResource("dashboard-2"),
			},
			supportedResources: []resources.Descriptor{dashboardDescriptor},
			clientShouldFail:   map[string]bool{"dashboard-1": true, "dashboard-2": true},
			clientFailureError: errors.New("delete API error"),
			wantSuccessCount:   0,
			wantFailedCount:    2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)

			mockClient := &mockDeleteClient{
				shouldFail:   tc.clientShouldFail,
				failureError: tc.clientFailureError,
			}

			mockRegistry := &mockPushRegistry{
				supportedResources: tc.supportedResources,
			}

			deleter := remote.NewDeleterWithClient(mockClient, mockRegistry)
			res := resources.NewResources(tc.resources...)

			summary, err := deleter.Delete(t.Context(), remote.DeleteRequest{
				Resources:      res,
				MaxConcurrency: 1,
				StopOnError:    tc.stopOnError,
			})

			if tc.wantErr {
				req.Error(err)
				req.NotNil(summary)
				req.Equal(tc.wantFailedCount, summary.FailedCount())
				return
			}

			req.NoError(err)
			req.NotNil(summary)
			req.Equal(tc.wantSuccessCount, summary.SuccessCount())
			req.Equal(tc.wantFailedCount, summary.FailedCount())

			if tc.wantDeletedNames != nil {
				req.ElementsMatch(tc.wantDeletedNames, mockClient.deletedNames)
			}
		})
	}
}

func TestDeleter_Delete_FailureDetails(t *testing.T) {
	dashboardDescriptor := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
		Kind:         "Dashboard",
		Singular:     "dashboard",
		Plural:       "dashboards",
	}

	deleteErr := errors.New("API error: resource locked")

	mockClient := &mockDeleteClient{
		shouldFail:   map[string]bool{"dashboard-bad": true},
		failureError: deleteErr,
	}

	mockRegistry := &mockPushRegistry{
		supportedResources: []resources.Descriptor{dashboardDescriptor},
	}

	res := resources.NewResources(
		createDashboardResource("dashboard-good"),
		createDashboardResource("dashboard-bad"),
	)

	deleter := remote.NewDeleterWithClient(mockClient, mockRegistry)

	summary, err := deleter.Delete(t.Context(), remote.DeleteRequest{
		Resources:      res,
		MaxConcurrency: 1,
	})

	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Equal(t, 1, summary.SuccessCount())
	require.Equal(t, 1, summary.FailedCount())

	failures := summary.Failures()
	require.Len(t, failures, 1)
	require.Equal(t, "dashboard-bad", failures[0].Resource.Name())
	require.Equal(t, deleteErr, failures[0].Error)
}
