package resources_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSelectors(t *testing.T) {
	tests := []struct {
		name string
		cmds []string
		want []resources.Selector
	}{
		{
			name: "should parse all resources of a type",
			cmds: []string{"dashboards"},
			want: []resources.Selector{
				{
					Type: resources.FilterTypeAll,
					GroupVersionKind: resources.PartialGVK{
						Group:    "",
						Version:  "",
						Resource: "dashboards",
					},
					ResourceUIDs: []string{},
				},
			},
		},
		{
			name: "should parse single resource",
			cmds: []string{"dashboards/foo"},
			want: []resources.Selector{
				{
					Type: resources.FilterTypeSingle,
					GroupVersionKind: resources.PartialGVK{
						Group:    "",
						Version:  "",
						Resource: "dashboards",
					},
					ResourceUIDs: []string{"foo"},
				},
			},
		},
		{
			name: "should parse multiple resources of the same type",
			cmds: []string{"dashboards/foo,bar"},
			want: []resources.Selector{
				{
					Type: resources.FilterTypeMultiple,
					GroupVersionKind: resources.PartialGVK{
						Group:    "",
						Version:  "",
						Resource: "dashboards",
					},
					ResourceUIDs: []string{"foo", "bar"},
				},
			},
		},
		{
			name: "should parse multiple resources with the same FQDN",
			cmds: []string{"dashboards.v1alpha1.dashboard.grafana.app/foo,bar"},
			want: []resources.Selector{
				{
					Type: resources.FilterTypeMultiple,
					GroupVersionKind: resources.PartialGVK{
						Group:         "dashboard.grafana.app",
						Version:       "v1alpha1",
						Resource:      "dashboards",
						FallbackGroup: "v1alpha1.dashboard.grafana.app",
					},
					ResourceUIDs: []string{"foo", "bar"},
				},
			},
		},
		{
			name: "should parse single resources of different types",
			cmds: []string{
				"dashboards/foo",
				"folders/bar",
			},
			want: []resources.Selector{
				{
					Type: resources.FilterTypeSingle,
					GroupVersionKind: resources.PartialGVK{
						Group:    "",
						Version:  "",
						Resource: "dashboards",
					},
					ResourceUIDs: []string{"foo"},
				},
				{
					Type: resources.FilterTypeSingle,
					GroupVersionKind: resources.PartialGVK{
						Group:    "",
						Version:  "",
						Resource: "folders",
					},
					ResourceUIDs: []string{"bar"},
				},
			},
		},
		{
			name: "should parse multiple resources of different types",
			cmds: []string{
				"dashboards/foo,bar",
				"folders/qux,quux",
			},
			want: []resources.Selector{
				{
					Type: resources.FilterTypeMultiple,
					GroupVersionKind: resources.PartialGVK{
						Group:    "",
						Version:  "",
						Resource: "dashboards",
					},
					ResourceUIDs: []string{"foo", "bar"},
				},
				{
					Type: resources.FilterTypeMultiple,
					GroupVersionKind: resources.PartialGVK{
						Group:    "",
						Version:  "",
						Resource: "folders",
					},
					ResourceUIDs: []string{"qux", "quux"},
				},
			},
		},
		{
			name: "should parse multiple resources of different types with mixed format",
			cmds: []string{
				"dashboards/foo,bar",
				"folders.folder/qux,quux",
			},
			want: []resources.Selector{
				{
					Type: resources.FilterTypeMultiple,
					GroupVersionKind: resources.PartialGVK{
						Group:    "",
						Version:  "",
						Resource: "dashboards",
					},
					ResourceUIDs: []string{"foo", "bar"},
				},
				{
					Type: resources.FilterTypeMultiple,
					GroupVersionKind: resources.PartialGVK{
						Group:    "folder",
						Version:  "",
						Resource: "folders",
					},
					ResourceUIDs: []string{"qux", "quux"},
				},
			},
		},
		{
			name: "should parse multiple FQDNs",
			cmds: []string{
				"dashboards.v1alpha1.dashboard.grafana.app/foo",
				"folders.v1alpha1.folder.grafana.app/bar",
			},
			want: []resources.Selector{
				{
					Type: resources.FilterTypeSingle,
					GroupVersionKind: resources.PartialGVK{
						Group:         "dashboard.grafana.app",
						Version:       "v1alpha1",
						Resource:      "dashboards",
						FallbackGroup: "v1alpha1.dashboard.grafana.app",
					},
					ResourceUIDs: []string{"foo"},
				},
				{
					Type: resources.FilterTypeSingle,
					GroupVersionKind: resources.PartialGVK{
						Group:         "folder.grafana.app",
						Version:       "v1alpha1",
						Resource:      "folders",
						FallbackGroup: "v1alpha1.folder.grafana.app",
					},
					ResourceUIDs: []string{"bar"},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resources.ParseSelectors(test.cmds)

			assert.ElementsMatch(t, test.want, got)
			assert.NoError(t, err)
		})
	}
}

func TestPartialGVK_ParseStringClearsFallback(t *testing.T) {
	for _, selector := range []string{"dashboards", "dashboards.dashboard"} {
		t.Run(selector, func(t *testing.T) {
			var gvk resources.PartialGVK
			require.NoError(t, gvk.ParseString("dashboards.dashboard.grafana.app"))
			candidate, ok := gvk.GroupOnlyCandidate()
			assert.True(t, ok)
			assert.Equal(t, "dashboard.grafana.app", candidate)
			require.NoError(t, gvk.ParseString(selector))
			_, ok = gvk.GroupOnlyCandidate()
			assert.False(t, ok, "reparsing must clear the previous ambiguous group")
		})
	}
}

func TestInvalidSelectorMessage(t *testing.T) {
	for _, src := range []string{"dashboards////", "dashboards/one/two"} {
		t.Run(src, func(t *testing.T) {
			_, err := resources.ParseSelectors([]string{src})
			require.Error(t, err)
			assert.Equal(t, "invalid resource selector \""+src+"\": expected a resource type optionally followed by /UID or /UID,UID; too many slash-separated segments", err.Error())
		})
	}
}
