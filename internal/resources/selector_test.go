package resources_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
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
						Group:    "dashboard.grafana.app",
						Version:  "v1alpha1",
						Resource: "dashboards",
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
						Group:    "dashboard.grafana.app",
						Version:  "v1alpha1",
						Resource: "dashboards",
					},
					ResourceUIDs: []string{"foo"},
				},
				{
					Type: resources.FilterTypeSingle,
					GroupVersionKind: resources.PartialGVK{
						Group:    "folder.grafana.app",
						Version:  "v1alpha1",
						Resource: "folders",
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
