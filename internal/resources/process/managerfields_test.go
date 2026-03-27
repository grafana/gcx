package process_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/process"
	"github.com/grafana/grafana/pkg/apimachinery/utils"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestManagerFieldsAppender(t *testing.T) {
	tests := []struct {
		name    string
		input   *resources.Resource
		want    unstructured.Unstructured
		wantErr bool
	}{
		{
			name:    "empty resource",
			input:   &resources.Resource{},
			want:    unstructured.Unstructured{},
			wantErr: false,
		},
		{
			name: "resource with no manager fields",
			input: resources.MustFromObject(
				map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name":      "example",
						"namespace": "default",
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
						"namespace": "default",
						"annotations": map[string]any{
							utils.AnnoKeyManagerKind:     string(resources.ResourceManagerKind),
							utils.AnnoKeyManagerIdentity: "gcx",
							utils.AnnoKeySourcePath:      "file://some/test/path.json",
						},
					},
					"spec": map[string]any{
						"title": "example",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "resource managed by Terraform",
			input: resources.MustFromObject(map[string]any{
				"apiVersion": "dashboard.grafana.app/v1",
				"kind":       "Dashboard",
				"metadata": map[string]any{
					"name":      "example",
					"namespace": "default",
					"annotations": map[string]any{
						utils.AnnoKeyManagerKind:     string(utils.ManagerKindTerraform),
						utils.AnnoKeyManagerIdentity: "terraform-version-1",
						utils.AnnoKeySourcePath:      "resource.tf",
					},
				},
				"spec": map[string]any{
					"title": "example",
				},
			}, resources.SourceInfo{}),
			want: unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name":      "example",
						"namespace": "default",
						"annotations": map[string]any{
							utils.AnnoKeyManagerKind:     string(utils.ManagerKindTerraform),
							utils.AnnoKeyManagerIdentity: "terraform-version-1",
							utils.AnnoKeySourcePath:      "resource.tf",
						},
					},
					"spec": map[string]any{
						"title": "example",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "resource managed by gcx",
			input: resources.MustFromObject(
				map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name":      "example",
						"namespace": "default",
						"annotations": map[string]any{
							utils.AnnoKeyManagerIdentity: "gcx",
							utils.AnnoKeyManagerKind:     string(resources.ResourceManagerKind),
							utils.AnnoKeySourcePath:      "file://some/test/path.json",
						},
					},
					"spec": map[string]any{
						"title": "example",
					},
				},
				resources.SourceInfo{
					Path: "other/test/path.json",
				},
			),
			want: unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "dashboard.grafana.app/v1",
					"kind":       "Dashboard",
					"metadata": map[string]any{
						"name":      "example",
						"namespace": "default",
						"annotations": map[string]any{
							utils.AnnoKeyManagerKind:     string(resources.ResourceManagerKind),
							utils.AnnoKeyManagerIdentity: "gcx",
							utils.AnnoKeySourcePath:      "file://other/test/path.json",
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
			appender := &process.ManagerFieldsAppender{}
			err := appender.Process(test.input)
			if test.wantErr {
				require.Error(t, err)
				return
			}

			require.Equal(t, test.want, test.input.ToUnstructured())
		})
	}
}
