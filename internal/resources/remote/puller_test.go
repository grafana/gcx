package remote_test

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// mockPullClient implements PullClient for testing.
type mockPullClient struct {
	// listResults maps descriptor plural to the items returned by List.
	listResults map[string][]unstructured.Unstructured
	// listErrors maps descriptor plural to the error returned by List.
	listErrors map[string]error
	// continueToken, when non-empty, is set on the returned list to simulate truncated results.
	continueToken string
}

func (m *mockPullClient) Get(
	_ context.Context, _ resources.Descriptor, _ string, _ metav1.GetOptions,
) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented in mock")
}

func (m *mockPullClient) GetMultiple(
	_ context.Context, _ resources.Descriptor, _ []string, _ metav1.GetOptions,
) ([]unstructured.Unstructured, error) {
	return nil, errors.New("not implemented in mock")
}

func (m *mockPullClient) List(
	_ context.Context, desc resources.Descriptor, _ metav1.ListOptions,
) (*unstructured.UnstructuredList, error) {
	if m.listErrors != nil {
		if err, ok := m.listErrors[desc.Plural]; ok {
			return nil, err
		}
	}

	items := m.listResults[desc.Plural]
	res := &unstructured.UnstructuredList{Items: items}
	if m.continueToken != "" {
		res.SetContinue(m.continueToken)
	}
	return res, nil
}

// mockPullRegistry implements PullRegistry for testing.
type mockPullRegistry struct {
	descriptors resources.Descriptors
}

func (m *mockPullRegistry) PreferredResources() resources.Descriptors {
	return m.descriptors
}

// makeUnstructuredDashboard creates a minimal unstructured dashboard for testing.
func makeUnstructuredDashboard(name string) unstructured.Unstructured {
	return unstructured.Unstructured{
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

// dashboardDescriptor returns a Descriptor for the dashboard resource type used in tests.
func dashboardDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
		Kind:         "Dashboard",
		Singular:     "dashboard",
		Plural:       "dashboards",
	}
}

func TestPuller_Pull(t *testing.T) {
	tests := []struct {
		name             string
		listResults      map[string][]unstructured.Unstructured
		listErrors       map[string]error
		stopOnError      bool
		limit            int64
		continueToken    string
		wantError        bool
		wantSuccessCount int
		wantFailedCount  int
		wantSkippedCount int
		wantTruncated    bool
	}{
		{
			name: "all resources succeed",
			listResults: map[string][]unstructured.Unstructured{
				"dashboards": {
					makeUnstructuredDashboard("dashboard-1"),
					makeUnstructuredDashboard("dashboard-2"),
					makeUnstructuredDashboard("dashboard-3"),
				},
			},
			stopOnError:      false,
			wantError:        false,
			wantSuccessCount: 3,
			wantFailedCount:  0,
		},
		{
			name:        "list failure with StopOnError=false records failure and returns nil error",
			listResults: map[string][]unstructured.Unstructured{},
			listErrors:  map[string]error{"dashboards": errors.New("connection refused")},
			stopOnError: false,
			wantError:   false,
			// The list-level failure counts as one failed operation.
			wantSuccessCount: 0,
			wantFailedCount:  1,
		},
		{
			name:        "list failure with StopOnError=true returns error",
			listResults: map[string][]unstructured.Unstructured{},
			listErrors:  map[string]error{"dashboards": errors.New("connection refused")},
			stopOnError: true,
			wantError:   true,
		},
		{
			name:             "empty list succeeds with zero counts",
			listResults:      map[string][]unstructured.Unstructured{"dashboards": {}},
			stopOnError:      false,
			wantError:        false,
			wantSuccessCount: 0,
			wantFailedCount:  0,
		},
		{
			name:        "404 NotFound is skipped, not counted as failure",
			listResults: map[string][]unstructured.Unstructured{},
			listErrors: map[string]error{
				"dashboards": apierrors.NewNotFound(
					schema.GroupResource{Group: "dashboard.grafana.app", Resource: "dashboards"}, ""),
			},
			stopOnError:      false,
			wantError:        false,
			wantSuccessCount: 0,
			wantFailedCount:  0,
			wantSkippedCount: 1,
		},
		{
			name:        "405 MethodNotAllowed is skipped, not counted as failure",
			listResults: map[string][]unstructured.Unstructured{},
			listErrors: map[string]error{
				"dashboards": apierrors.NewMethodNotSupported(
					schema.GroupResource{Group: "datasource.grafana.app", Resource: "queryconvert"}, "list"),
			},
			stopOnError:      false,
			wantError:        false,
			wantSuccessCount: 0,
			wantFailedCount:  0,
			wantSkippedCount: 1,
		},
		{
			name:        "404 NotFound with StopOnError=true is skipped, not an error",
			listResults: map[string][]unstructured.Unstructured{},
			listErrors: map[string]error{
				"dashboards": apierrors.NewNotFound(
					schema.GroupResource{Group: "dashboard.grafana.app", Resource: "dashboards"}, ""),
			},
			stopOnError:      true,
			wantError:        false,
			wantSuccessCount: 0,
			wantFailedCount:  0,
			wantSkippedCount: 1,
		},
		{
			name:        "403 Forbidden is still counted as failure",
			listResults: map[string][]unstructured.Unstructured{},
			listErrors: map[string]error{
				"dashboards": apierrors.NewForbidden(
					schema.GroupResource{Group: "dashboard.grafana.app", Resource: "dashboards"}, "list",
					errors.New("access denied")),
			},
			stopOnError:      false,
			wantError:        false,
			wantSuccessCount: 0,
			wantFailedCount:  1,
			wantSkippedCount: 0,
		},
		{
			name: "list with limit and more results available",
			listResults: map[string][]unstructured.Unstructured{
				"dashboards": {
					makeUnstructuredDashboard("d-1"),
					makeUnstructuredDashboard("d-2"),
					makeUnstructuredDashboard("d-3"),
					makeUnstructuredDashboard("d-4"),
					makeUnstructuredDashboard("d-5"),
				},
			},
			limit:            5,
			continueToken:    "next-page-token",
			wantSuccessCount: 5,
			wantTruncated:    true,
		},
		{
			name: "list with limit and no more results",
			listResults: map[string][]unstructured.Unstructured{
				"dashboards": {
					makeUnstructuredDashboard("d-1"),
					makeUnstructuredDashboard("d-2"),
					makeUnstructuredDashboard("d-3"),
				},
			},
			limit:            5,
			wantSuccessCount: 3,
			wantTruncated:    false,
		},
		{
			name: "list without limit is not truncated",
			listResults: map[string][]unstructured.Unstructured{
				"dashboards": {
					makeUnstructuredDashboard("d-1"),
					makeUnstructuredDashboard("d-2"),
					makeUnstructuredDashboard("d-3"),
					makeUnstructuredDashboard("d-4"),
					makeUnstructuredDashboard("d-5"),
					makeUnstructuredDashboard("d-6"),
					makeUnstructuredDashboard("d-7"),
					makeUnstructuredDashboard("d-8"),
					makeUnstructuredDashboard("d-9"),
					makeUnstructuredDashboard("d-10"),
				},
			},
			wantSuccessCount: 10,
			wantTruncated:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)

			mockClient := &mockPullClient{
				listResults:   tc.listResults,
				listErrors:    tc.listErrors,
				continueToken: tc.continueToken,
			}

			desc := dashboardDescriptor()
			mockRegistry := &mockPullRegistry{
				descriptors: resources.Descriptors{desc},
			}

			puller := remote.NewPuller(mockClient, mockRegistry)

			dest := resources.NewResources()
			pullReq := remote.PullRequest{
				Filters: resources.Filters{
					{
						Type:       resources.FilterTypeAll,
						Descriptor: desc,
					},
				},
				Resources:   dest,
				StopOnError: tc.stopOnError,
				Limit:       tc.limit,
			}

			summary, err := puller.Pull(context.Background(), pullReq)

			if tc.wantError {
				req.Error(err)
				return
			}

			req.NoError(err)
			req.NotNil(summary)
			req.Equal(tc.wantSuccessCount, summary.SuccessCount())
			req.Equal(tc.wantFailedCount, summary.FailedCount())
			req.Equal(tc.wantSkippedCount, summary.SkippedCount())
			req.Len(summary.Failures(), tc.wantFailedCount)

			// For failure cases, verify the failure has a nil resource (filter-level failure)
			// and a non-nil error.
			if tc.wantFailedCount > 0 {
				for _, failure := range summary.Failures() {
					req.Nil(failure.Resource, "filter-level failure should have nil resource")
					req.Error(failure.Error)
				}
			}

			// Verify truncation tracking.
			req.Equal(tc.wantTruncated, summary.IsTruncated())

			// For success cases, verify the destination received the resources.
			if tc.wantSuccessCount > 0 {
				req.Equal(tc.wantSuccessCount, dest.Len())
			}
		})
	}
}
