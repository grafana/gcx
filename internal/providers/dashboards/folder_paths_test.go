package dashboards_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/dashboards"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func makeFolder(name, title, parentUID string) unstructured.Unstructured {
	folder := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "folder.grafana.app/v1beta1",
		"kind":       "Folder",
		"metadata": map[string]any{
			"name": name,
		},
		"spec": map[string]any{
			"title": title,
		},
	}}
	if parentUID != "" {
		folder.SetAnnotations(map[string]string{"grafana.app/folder": parentUID})
	}
	return folder
}

func TestBuildFolderPathMap(t *testing.T) {
	paths := dashboards.BuildFolderPathMapForTest([]unstructured.Unstructured{
		makeFolder("root-uid", "Root", ""),
		makeFolder("child-uid", "Child", "root-uid"),
		makeFolder("grandchild-uid", "Grandchild", "child-uid"),
	})

	want := map[string]string{
		"root-uid":       "Root",
		"child-uid":      "Root/Child",
		"grandchild-uid": "Root/Child/Grandchild",
	}
	for uid, expected := range want {
		if paths[uid] != expected {
			t.Fatalf("paths[%q] = %q, want %q", uid, paths[uid], expected)
		}
	}
}
