package local_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestFSWriter_Write(t *testing.T) {
	req := require.New(t)
	outputDir := filepath.Join(t.TempDir(), "output")

	writer := local.FSWriter{
		Path:    outputDir,
		Encoder: format.NewYAMLCodec(),
		Namer: func(resource *resources.Resource) (string, error) {
			return resource.Name() + ".yaml", nil
		},
	}

	err := writer.Write(t.Context(), testResources())
	req.NoError(err)

	req.FileExists(filepath.Join(outputDir, "folder-uid.yaml"))
	req.FileExists(filepath.Join(outputDir, "sa-uid.yaml"))
}

func TestFSWriter_Write_continueOnError(t *testing.T) {
	req := require.New(t)
	outputDir := filepath.Join(t.TempDir(), "output")

	writer := local.FSWriter{
		Path:        outputDir,
		Encoder:     format.NewYAMLCodec(),
		StopOnError: false,
		Namer: func(resource *resources.Resource) (string, error) {
			if resource.Kind() == "Folder" {
				return "", errors.New("woops, folders are causing some trouble :(")
			}
			return resource.Name() + ".yaml", nil
		},
	}

	err := writer.Write(t.Context(), testResources())
	req.NoError(err)

	req.NoFileExists(filepath.Join(outputDir, "folder-uid.yaml"), "not created because of an error somewhere")
	req.FileExists(filepath.Join(outputDir, "sa-uid.yaml"), "continued on error and got created")
}

func TestFSWriter_Write_groupedByKind(t *testing.T) {
	req := require.New(t)
	outputDir := filepath.Join(t.TempDir(), "output")

	writer := local.FSWriter{
		Path:    outputDir,
		Encoder: format.NewJSONCodec(),
		Namer:   local.GroupResourcesByKind("json", nil),
	}

	err := writer.Write(t.Context(), testResources())
	req.NoError(err)

	req.FileExists(filepath.Join(outputDir, "folders.v0alpha1.folder.grafana.app", "folder-uid.json"))
	req.FileExists(filepath.Join(outputDir, "serviceaccounts.v0alpha1.iam.grafana.app", "sa-uid.json"))
}

func TestFSWriter_Write_doesNothingWithNoResources(t *testing.T) {
	req := require.New(t)
	outputDir := filepath.Join(t.TempDir(), "output")
	input, err := resources.NewResourcesFromUnstructured(unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{},
	})
	req.NoError(err)

	writer := local.FSWriter{
		Path:    outputDir,
		Encoder: format.NewYAMLCodec(),
		Namer: func(resource *resources.Resource) (string, error) {
			return resource.Name() + ".yaml", nil
		},
	}

	err = writer.Write(t.Context(), input)
	req.NoError(err)

	req.NoDirExists(outputDir)
}

func testResources() *resources.Resources {
	res, err := resources.NewResourcesFromUnstructured(unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			{
				Object: map[string]any{
					"apiVersion": "folder.grafana.app/v0alpha1",
					"kind":       "Folder",
					"metadata": map[string]any{
						"name":      "folder-uid",
						"namespace": "default",
					},
					"spec": map[string]any{
						"title": "Test folder",
					},
				},
			},
			{
				Object: map[string]any{
					"apiVersion": "iam.grafana.app/v0alpha1",
					"kind":       "ServiceAccount",
					"metadata": map[string]any{
						"name":      "sa-uid",
						"namespace": "default",
					},
					"spec": map[string]any{
						"title": "editor",
					},
				},
			},
		},
	})

	if err != nil {
		panic(err)
	}

	return res
}
