package dashboards_test

import (
	"encoding/json"
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

func TestDashboardSummaryUsesFolderPathAndKeepsUID(t *testing.T) {
	item := makeItem("dash-1", "dashboard.grafana.app/v2", "Dash 1", "child-uid", nil, nil, nil)
	list := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{item}}

	summary := dashboards.DashboardListOutputValueWithFolderPathsForTest(list, "json", map[string]string{
		"child-uid": "Root/Child",
	})

	encoded := mustJSONMap(t, summary)
	items := mustSlice(t, encoded["items"])
	first := mustMap(t, items[0])
	spec := mustMap(t, first["spec"])
	if spec["folder"] != "Root/Child" {
		t.Fatalf("spec.folder = %v, want Root/Child", spec["folder"])
	}
	if spec["folderUID"] != "child-uid" {
		t.Fatalf("spec.folderUID = %v, want child-uid", spec["folderUID"])
	}
}

func mustJSONMap(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal summary: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal summary: %v", err)
	}
	return out
}
