package process_test

import (
	"testing"

	"github.com/grafana/gcx/internal/grafanameta"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/process"
	"github.com/grafana/gcx/internal/version"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestManagerIdentity(t *testing.T) {
	// The manager identity must be the same for every gcx build.
	require.Equal(t, "gcx", process.ManagerIdentity())

	version.Set("1.2.3")
	t.Cleanup(func() { version.Set("") })

	require.Equal(t, "gcx", process.ManagerIdentity())
}

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
							grafanameta.AnnoKeyManagerKind:        string(resources.ResourceManagerKind),
							grafanameta.AnnoKeyManagerIdentity:    process.ManagerIdentity(),
							grafanameta.AnnoKeyManagerAllowsEdits: "true",
							grafanameta.AnnoKeySourcePath:         "file://some/test/path.json",
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
						grafanameta.AnnoKeyManagerKind:     string(grafanameta.ManagerKindTerraform),
						grafanameta.AnnoKeyManagerIdentity: "terraform-version-1",
						grafanameta.AnnoKeySourcePath:      "resource.tf",
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
							grafanameta.AnnoKeyManagerKind:     string(grafanameta.ManagerKindTerraform),
							grafanameta.AnnoKeyManagerIdentity: "terraform-version-1",
							grafanameta.AnnoKeySourcePath:      "resource.tf",
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
							grafanameta.AnnoKeyManagerIdentity: "gcx", // stale value from a previous push
							grafanameta.AnnoKeyManagerKind:     string(resources.ResourceManagerKind),
							grafanameta.AnnoKeySourcePath:      "file://some/test/path.json",
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
							grafanameta.AnnoKeyManagerKind:        string(resources.ResourceManagerKind),
							grafanameta.AnnoKeyManagerIdentity:    process.ManagerIdentity(),
							grafanameta.AnnoKeyManagerAllowsEdits: "true",
							grafanameta.AnnoKeySourcePath:         "file://other/test/path.json",
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
