package resources_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestFilter_Matches(t *testing.T) {
	dashboardDescriptor := resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "dashboard.grafana.app",
			Version: "v1",
		},
		Kind:     "Dashboard",
		Singular: "dashboard",
		Plural:   "dashboards",
	}

	dashboard := resources.MustFromObject(map[string]any{
		"apiVersion": dashboardDescriptor.GroupVersion.String(),
		"kind":       dashboardDescriptor.Kind,
		"metadata": map[string]any{
			"name": "test-1",
		},
		"spec": map[string]any{
			"uid": "test-1",
		},
	}, resources.SourceInfo{})

	folder := resources.MustFromObject(map[string]any{
		"apiVersion": "folder.grafana.app/v1",
		"kind":       "Folder",
		"metadata": map[string]any{
			"name": "test-3",
		},
		"spec": map[string]any{
			"title": "test-3",
		},
	}, resources.SourceInfo{})

	tests := []struct {
		name     string
		filter   resources.Filter
		resource *resources.Resource
		want     bool
	}{
		{
			name: "all filter matches all resources",
			filter: resources.Filter{
				Type:       resources.FilterTypeAll,
				Descriptor: dashboardDescriptor,
			},
			resource: dashboard,
			want:     true,
		},
		{
			name: "multiple filter matches resources with matching UID",
			filter: resources.Filter{
				Type:         resources.FilterTypeMultiple,
				Descriptor:   dashboardDescriptor,
				ResourceUIDs: []string{"test-1", "test-2"},
			},
			resource: dashboard,
			want:     true,
		},
		{
			name: "multiple filter does not match resources with non-matching UID",
			filter: resources.Filter{
				Type:         resources.FilterTypeMultiple,
				Descriptor:   dashboardDescriptor,
				ResourceUIDs: []string{"test-2", "test-3"},
			},
			resource: dashboard,
			want:     false,
		},
		{
			name: "single filter matches resources with matching UID",
			filter: resources.Filter{
				Type:         resources.FilterTypeSingle,
				Descriptor:   dashboardDescriptor,
				ResourceUIDs: []string{"test-1"},
			},
			resource: dashboard,
			want:     true,
		},
		{
			name: "single filter does not match resources with non-matching UID",
			filter: resources.Filter{
				Type:         resources.FilterTypeSingle,
				Descriptor:   dashboardDescriptor,
				ResourceUIDs: []string{"test-2"},
			},
			resource: dashboard,
			want:     false,
		},
		{
			name: "filter does not match resources with different descriptor",
			filter: resources.Filter{
				Type:       resources.FilterTypeAll,
				Descriptor: dashboardDescriptor,
			},
			resource: folder,
			want:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.filter.Matches(*test.resource)
			if got != test.want {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}
