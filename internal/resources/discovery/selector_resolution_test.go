package discovery_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestRegistry_ParsedSelectors(t *testing.T) {
	groups, lists := getMixedVersionsDiscovery()
	reg, err := discovery.NewRegistry(t.Context(), &mockDiscoveryClient{groups: groups, resources: lists})
	require.NoError(t, err)

	dashboard := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v2"},
		Kind:         "Dashboard", Singular: "dashboard", Plural: "dashboards",
	}
	dashboardV1 := dashboard
	dashboardV1.GroupVersion.Version = "v1"
	routing := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "notifications.alerting.grafana.app", Version: "v1beta1"},
		Kind:         "RoutingTree", Singular: "routingtree", Plural: "routingtrees",
	}
	slo := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "slo.ext.grafana.app", Version: "v1alpha1"},
		Kind:         "SLO", Singular: "slo", Plural: "slos",
	}
	versionGroup := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "v1.example.com", Version: "v2"},
		Kind:         "Widget", Singular: "widget", Plural: "widgets",
	}
	customVersion := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "example.com", Version: "release"},
		Kind:         "Widget", Singular: "widget", Plural: "widgets",
	}
	for _, desc := range []resources.Descriptor{routing, slo, versionGroup, customVersion} {
		reg.RegisterAdapter(nil, desc, nil)
	}

	tests := []struct {
		selector  string
		preferred resources.Descriptors
		all       resources.Descriptors
	}{
		{"dashboards", resources.Descriptors{dashboard}, resources.Descriptors{dashboardV1, dashboard}},
		{"dashboards.dashboard", resources.Descriptors{dashboard}, resources.Descriptors{dashboardV1, dashboard}},
		{"dashboards.dashboard.grafana.app", resources.Descriptors{dashboard}, resources.Descriptors{dashboardV1, dashboard}},
		{"dashboards.v1.dashboard.grafana.app", resources.Descriptors{dashboardV1}, resources.Descriptors{dashboardV1}},
		{"dashboards.v1.dashboard", resources.Descriptors{dashboardV1}, resources.Descriptors{dashboardV1}},
		{"routingtrees.notifications", resources.Descriptors{routing}, resources.Descriptors{routing}},
		{"routingtrees.notifications.alerting.grafana.app", resources.Descriptors{routing}, resources.Descriptors{routing}},
		{"routingtrees.v1beta1.notifications.alerting.grafana.app", resources.Descriptors{routing}, resources.Descriptors{routing}},
		{"slos.slo.ext.grafana.app", resources.Descriptors{slo}, resources.Descriptors{slo}},
		{"slos.v1alpha1.slo.ext.grafana.app", resources.Descriptors{slo}, resources.Descriptors{slo}},
		{"widgets.v1.example.com", resources.Descriptors{versionGroup}, resources.Descriptors{versionGroup}},
		{"widgets.release.example.com", resources.Descriptors{customVersion}, resources.Descriptors{customVersion}},
	}
	for _, tt := range tests {
		for _, suffix := range []string{"", "/foo", "/foo,bar"} {
			for _, preferredOnly := range []bool{true, false} {
				t.Run(tt.selector+suffix+map[bool]string{true: "/preferred", false: "/all"}[preferredOnly], func(t *testing.T) {
					sels, err := resources.ParseSelectors([]string{tt.selector + suffix})
					require.NoError(t, err)
					filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{Selectors: sels, PreferredVersionOnly: preferredOnly})
					require.NoError(t, err)
					want := tt.all
					if preferredOnly {
						want = tt.preferred
					}
					var descs resources.Descriptors
					for _, filter := range filters {
						descs = append(descs, filter.Descriptor)
						assert.Equal(t, sels[0].Type, filter.Type)
						assert.Equal(t, sels[0].ResourceUIDs, filter.ResourceUIDs)
					}
					assert.ElementsMatch(t, want, descs)
				})
			}
		}
	}
}

func TestRegistry_SelectorCandidatePriority(t *testing.T) {
	reg := discovery.NewStaticRegistry()
	versioned := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "example.com", Version: "v1"},
		Kind:         "Widget", Singular: "widget", Plural: "widgets",
	}
	groupOnly := versioned
	groupOnly.GroupVersion.Group = "v1.example.com"
	groupOnly.GroupVersion.Version = "v2"
	reg.RegisterAdapter(nil, groupOnly, nil)
	reg.RegisterAdapter(nil, versioned, nil)
	for _, preferredOnly := range []bool{true, false} {
		t.Run(map[bool]string{true: "preferred", false: "all"}[preferredOnly], func(t *testing.T) {
			sels, err := resources.ParseSelectors([]string{"widgets.v1.example.com/foo"})
			require.NoError(t, err)
			filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{Selectors: sels, PreferredVersionOnly: preferredOnly})
			require.NoError(t, err)
			require.Len(t, filters, 1)
			assert.Equal(t, versioned, filters[0].Descriptor)
		})
	}
}

func TestRegistry_UnsupportedSelectorCandidates(t *testing.T) {
	for _, tt := range []struct {
		selector string
		wantErr  string
	}{
		{
			selector: "missing.dashboard.grafana.app/foo,bar",
			wantErr:  `the server does not support this resource (resource "missing" is not served by group "grafana.app" at version "dashboard", nor by group "dashboard.grafana.app")`,
		},
		{
			selector: "missing.v1.dashboard.grafana.app/foo",
			wantErr:  `the server does not support this resource (resource "missing" is not served by group "dashboard.grafana.app" at version "v1", nor by group "v1.dashboard.grafana.app")`,
		},
	} {
		t.Run(tt.selector, func(t *testing.T) {
			sels, err := resources.ParseSelectors([]string{tt.selector})
			require.NoError(t, err)
			_, err = discovery.NewStaticRegistry().MakeFilters(discovery.MakeFiltersOptions{Selectors: sels})
			require.Error(t, err)
			var invalid resources.InvalidSelectorError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tt.selector, invalid.Command)
			assert.Equal(t, tt.wantErr, invalid.Err)
		})
	}
}

func TestRegistryIndex_StructuredGVKRequiresExactVersion(t *testing.T) {
	idx := discovery.NewRegistryIndex()
	idx.RegisterStatic(resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "v3.dashboard.grafana.app", Version: "v1"},
		Kind:         "Dashboard", Singular: "dashboard", Plural: "dashboards",
	}, nil)
	gvk := resources.PartialGVK{Resource: "dashboards", Group: "dashboard.grafana.app", Version: "v3"}
	for _, lookup := range []struct {
		name  string
		found func() bool
	}{
		{"single", func() bool { _, ok := idx.LookupPartialGVK(gvk); return ok }},
		{"preferred", func() bool { _, ok := idx.LookupPreferredPerGroup(gvk); return ok }},
		{"all", func() bool { _, ok := idx.LookupAllVersionsForPartialGVK(gvk); return ok }},
	} {
		t.Run(lookup.name, func(t *testing.T) {
			assert.False(t, lookup.found(), "an explicit group/version must not fall back to a different group")
		})
	}
}
