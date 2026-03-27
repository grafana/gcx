package process_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/process"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNamespaceOverrider(t *testing.T) {
	tests := []struct {
		name            string
		targetNamespace string
		input           *resources.Resource
		want            unstructured.Unstructured
		wantErr         bool
	}{
		{
			name:            "empty resource",
			targetNamespace: "target-namespace",
			input:           &resources.Resource{},
			want:            unstructured.Unstructured{},
			wantErr:         false,
		},
		{
			name:            "resource with different namespace",
			targetNamespace: "target-namespace",
			input: resources.MustFromObject(
				map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name":      "example",
						"namespace": "source-namespace",
					},
					"spec": map[string]any{
						"title": "example",
					},
				},
				resources.SourceInfo{
					Path: "some/test/path.json",
				},
			),
			want: unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name":      "example",
						"namespace": "target-namespace",
					},
					"spec": map[string]any{
						"title": "example",
					},
				},
			},
			wantErr: false,
		},
		{
			name:            "resource with same namespace (no-op)",
			targetNamespace: "same-namespace",
			input: resources.MustFromObject(
				map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name":      "example",
						"namespace": "same-namespace",
					},
					"spec": map[string]any{
						"title": "example",
					},
				},
				resources.SourceInfo{
					Path: "some/test/path.json",
				},
			),
			want: unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name":      "example",
						"namespace": "same-namespace",
					},
					"spec": map[string]any{
						"title": "example",
					},
				},
			},
			wantErr: false,
		},
		{
			name:            "resource without namespace field",
			targetNamespace: "target-namespace",
			input: resources.MustFromObject(
				map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name": "example",
					},
					"spec": map[string]any{
						"title": "example",
					},
				},
				resources.SourceInfo{
					Path: "some/test/path.json",
				},
			),
			want: unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name":      "example",
						"namespace": "target-namespace",
					},
					"spec": map[string]any{
						"title": "example",
					},
				},
			},
			wantErr: false,
		},
		{
			name:            "resource with metadata fields and annotations",
			targetNamespace: "new-namespace",
			input: resources.MustFromObject(
				map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name":      "example",
						"namespace": "old-namespace",
						"labels": map[string]any{
							"app": "test",
						},
						"annotations": map[string]any{
							"description": "test dashboard",
						},
					},
					"spec": map[string]any{
						"title": "example",
					},
				},
				resources.SourceInfo{
					Path: "some/test/path.json",
				},
			),
			want: unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name":      "example",
						"namespace": "new-namespace",
						"labels": map[string]any{
							"app": "test",
						},
						"annotations": map[string]any{
							"description": "test dashboard",
						},
					},
					"spec": map[string]any{
						"title": "example",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overrider := process.NewNamespaceOverrider(test.targetNamespace)
			err := overrider.Process(test.input)
			if test.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, test.want, test.input.ToUnstructured())
		})
	}
}
