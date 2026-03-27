package discovery_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestRegistry_MakeFilters(t *testing.T) {
	tests := []struct {
		name      string
		discovery func() ([]*metav1.APIGroup, []*metav1.APIResourceList)
		selectors resources.Selectors
		want      resources.Filters
		wantErr   bool
	}{
		{
			name:      "lookup with resource returns all supported versions",
			discovery: getMixedVersionsDiscovery,
			selectors: resources.Selectors{
				{
					Type: resources.FilterTypeMultiple,
					GroupVersionKind: resources.PartialGVK{
						Resource: "dashboards",
					},
					ResourceUIDs: []string{"foo", "bar"},
				},
			},
			want: resources.Filters{
				{
					Type:         resources.FilterTypeMultiple,
					ResourceUIDs: []string{"foo", "bar"},
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v1",
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
				{
					Type:         resources.FilterTypeMultiple,
					ResourceUIDs: []string{"foo", "bar"},
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v2",
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
			},
			wantErr: false,
		},
		{
			name:      "lookup with non-existent resource returns error",
			discovery: getMixedVersionsDiscovery,
			selectors: resources.Selectors{
				{
					Type: resources.FilterTypeMultiple,
					GroupVersionKind: resources.PartialGVK{
						Resource: "nonexistent",
					},
				},
			},
			want:    resources.Filters{},
			wantErr: true,
		},
		{
			name:      "lookup with single filter type returns all versions",
			discovery: getMixedVersionsDiscovery,
			selectors: resources.Selectors{
				{
					Type: resources.FilterTypeSingle,
					GroupVersionKind: resources.PartialGVK{
						Resource: "dashboards",
					},
					ResourceUIDs: []string{"single-dashboard"},
				},
			},
			want: resources.Filters{
				{
					Type:         resources.FilterTypeSingle,
					ResourceUIDs: []string{"single-dashboard"},
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v1",
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
				{
					Type:         resources.FilterTypeSingle,
					ResourceUIDs: []string{"single-dashboard"},
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v2",
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
			},
			wantErr: false,
		},
		{
			name:      "lookup with all filter type returns all versions",
			discovery: getMixedVersionsDiscovery,
			selectors: resources.Selectors{
				{
					Type: resources.FilterTypeAll,
					GroupVersionKind: resources.PartialGVK{
						Resource: "folders",
					},
				},
			},
			want: resources.Filters{
				{
					Type: resources.FilterTypeAll,
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "folder.grafana.app",
							Version: "v1",
						},
						Kind:     "Folder",
						Plural:   "folders",
						Singular: "folder",
					},
				},
			},
			wantErr: false,
		},
		{
			name:      "lookup with specific group and version returns only that version",
			discovery: getMixedVersionsDiscovery,
			selectors: resources.Selectors{
				{
					Type: resources.FilterTypeAll,
					GroupVersionKind: resources.PartialGVK{
						Group:    "dashboard.grafana.app",
						Version:  "v1",
						Resource: "dashboards",
					},
				},
			},
			want: resources.Filters{
				{
					Type: resources.FilterTypeAll,
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v1",
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
			},
			wantErr: false,
		},
		{
			name:      "lookup with multiple selectors returns all versions for each",
			discovery: getMixedVersionsDiscovery,
			selectors: resources.Selectors{
				{
					Type: resources.FilterTypeSingle,
					GroupVersionKind: resources.PartialGVK{
						Resource: "dashboards",
					},
					ResourceUIDs: []string{"dashboard-1"},
				},
				{
					Type: resources.FilterTypeAll,
					GroupVersionKind: resources.PartialGVK{
						Resource: "folders",
					},
				},
			},
			want: resources.Filters{
				{
					Type:         resources.FilterTypeSingle,
					ResourceUIDs: []string{"dashboard-1"},
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v1",
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
				{
					Type:         resources.FilterTypeSingle,
					ResourceUIDs: []string{"dashboard-1"},
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v2",
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
				{
					Type: resources.FilterTypeAll,
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "folder.grafana.app",
							Version: "v1",
						},
						Kind:     "Folder",
						Plural:   "folders",
						Singular: "folder",
					},
				},
			},
			wantErr: false,
		},
		{
			name:      "lookup with kind instead of resource returns all versions",
			discovery: getMixedVersionsDiscovery,
			selectors: resources.Selectors{
				{
					Type: resources.FilterTypeAll,
					GroupVersionKind: resources.PartialGVK{
						Resource: "Dashboard",
					},
				},
			},
			want: resources.Filters{
				{
					Type: resources.FilterTypeAll,
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v1",
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
				{
					Type: resources.FilterTypeAll,
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v2",
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groups, resources := test.discovery()
			client := &mockDiscoveryClient{
				groups:    groups,
				resources: resources,
			}

			reg, err := discovery.NewRegistry(t.Context(), client)
			require.NoError(t, err)

			got, err := reg.MakeFilters(discovery.MakeFiltersOptions{
				Selectors:            test.selectors,
				PreferredVersionOnly: false,
			})
			if test.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.ElementsMatch(t, test.want, got)
		})
	}
}

func TestRegistry_MakeFilters_WithOptions(t *testing.T) {
	tests := []struct {
		name                   string
		discovery              func() ([]*metav1.APIGroup, []*metav1.APIResourceList)
		selectors              resources.Selectors
		preferPreferredVersion bool
		want                   resources.Filters
		wantErr                bool
	}{
		{
			name:                   "preferPreferredVersion=true returns only preferred version",
			discovery:              getMixedVersionsDiscovery,
			preferPreferredVersion: true,
			selectors: resources.Selectors{
				{
					Type: resources.FilterTypeMultiple,
					GroupVersionKind: resources.PartialGVK{
						Resource: "dashboards",
					},
					ResourceUIDs: []string{"foo", "bar"},
				},
			},
			want: resources.Filters{
				{
					Type:         resources.FilterTypeMultiple,
					ResourceUIDs: []string{"foo", "bar"},
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v2", // This should be the preferred version
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
			},
			wantErr: false,
		},
		{
			name:                   "preferPreferredVersion=false returns all versions",
			discovery:              getMixedVersionsDiscovery,
			preferPreferredVersion: false,
			selectors: resources.Selectors{
				{
					Type: resources.FilterTypeMultiple,
					GroupVersionKind: resources.PartialGVK{
						Resource: "dashboards",
					},
					ResourceUIDs: []string{"foo", "bar"},
				},
			},
			want: resources.Filters{
				{
					Type:         resources.FilterTypeMultiple,
					ResourceUIDs: []string{"foo", "bar"},
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v1",
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
				{
					Type:         resources.FilterTypeMultiple,
					ResourceUIDs: []string{"foo", "bar"},
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v2",
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
			},
			wantErr: false,
		},
		{
			name:                   "specific version provided ignores preferPreferredVersion",
			discovery:              getMixedVersionsDiscovery,
			preferPreferredVersion: true,
			selectors: resources.Selectors{
				{
					Type: resources.FilterTypeMultiple,
					GroupVersionKind: resources.PartialGVK{
						Resource: "dashboards",
						Version:  "v1",
					},
					ResourceUIDs: []string{"foo", "bar"},
				},
			},
			want: resources.Filters{
				{
					Type:         resources.FilterTypeMultiple,
					ResourceUIDs: []string{"foo", "bar"},
					Descriptor: resources.Descriptor{
						GroupVersion: schema.GroupVersion{
							Group:   "dashboard.grafana.app",
							Version: "v1", // Should return the specific version requested
						},
						Kind:     "Dashboard",
						Plural:   "dashboards",
						Singular: "dashboard",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groups, resources := test.discovery()
			client := &mockDiscoveryClient{
				groups:    groups,
				resources: resources,
			}

			reg, err := discovery.NewRegistry(t.Context(), client)
			require.NoError(t, err)

			got, err := reg.MakeFilters(discovery.MakeFiltersOptions{
				Selectors:            test.selectors,
				PreferredVersionOnly: test.preferPreferredVersion,
			})
			if test.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.ElementsMatch(t, test.want, got)
		})
	}
}

func TestRegistry_PreferredResources(t *testing.T) {
	tests := []struct {
		name      string
		discovery func() ([]*metav1.APIGroup, []*metav1.APIResourceList)
		want      resources.Descriptors
	}{
		{
			name:      "returns preferred versions of all resources",
			discovery: getMixedVersionsDiscovery,
			want: resources.Descriptors{
				{
					Kind:     "Dashboard",
					Plural:   "dashboards",
					Singular: "dashboard",
					GroupVersion: schema.GroupVersion{
						Group:   "dashboard.grafana.app",
						Version: "v2",
					},
				},
				{
					Kind:     "Folder",
					Plural:   "folders",
					Singular: "folder",
					GroupVersion: schema.GroupVersion{
						Group:   "folder.grafana.app",
						Version: "v1",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groups, resources := test.discovery()
			client := &mockDiscoveryClient{
				groups:    groups,
				resources: resources,
			}

			reg, err := discovery.NewRegistry(t.Context(), client)
			require.NoError(t, err)

			got := reg.PreferredResources()
			assert.ElementsMatch(t, test.want, got)
		})
	}
}

func TestRegistry_SupportedResources(t *testing.T) {
	tests := []struct {
		name      string
		discovery func() ([]*metav1.APIGroup, []*metav1.APIResourceList)
		want      resources.Descriptors
	}{
		{
			name:      "returns all supported resources",
			discovery: getMixedVersionsDiscovery,
			want: resources.Descriptors{
				{
					Kind:     "Dashboard",
					Plural:   "dashboards",
					Singular: "dashboard",
					GroupVersion: schema.GroupVersion{
						Group:   "dashboard.grafana.app",
						Version: "v1",
					},
				},
				{
					Kind:     "Dashboard",
					Plural:   "dashboards",
					Singular: "dashboard",
					GroupVersion: schema.GroupVersion{
						Group:   "dashboard.grafana.app",
						Version: "v2",
					},
				},
				{
					Kind:     "Folder",
					Plural:   "folders",
					Singular: "folder",
					GroupVersion: schema.GroupVersion{
						Group:   "folder.grafana.app",
						Version: "v1",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groups, resources := test.discovery()
			client := &mockDiscoveryClient{
				groups:    groups,
				resources: resources,
			}

			reg, err := discovery.NewRegistry(t.Context(), client)
			require.NoError(t, err)

			got := reg.SupportedResources()
			assert.ElementsMatch(t, test.want, got)
		})
	}
}

func TestRegistry_Discover(t *testing.T) {
	tests := []struct {
		name      string
		discovery func() ([]*metav1.APIGroup, []*metav1.APIResourceList)
		err       error
		wantErr   bool
	}{
		{
			name:      "successful discovery",
			discovery: getMixedVersionsDiscovery,
			err:       nil,
			wantErr:   false,
		},
		{
			name:      "discovery error",
			discovery: getMixedVersionsDiscovery,
			err:       assert.AnError,
			wantErr:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groups, resources := test.discovery()
			client := &mockDiscoveryClient{
				groups:    groups,
				resources: resources,
				err:       test.err,
			}

			reg, err := discovery.NewRegistry(t.Context(), client)
			discoverErr := reg.Discover(t.Context())

			if test.wantErr {
				require.Error(t, err)
				require.Error(t, discoverErr)
				return
			}

			require.NoError(t, err)
			require.NoError(t, discoverErr)
		})
	}
}

type mockDiscoveryClient struct {
	groups    []*metav1.APIGroup
	resources []*metav1.APIResourceList
	err       error
}

func (m *mockDiscoveryClient) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return m.groups, m.resources, m.err
}
