package dashboards_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/dashboards"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDashboardListSummaryOmitsDashboardBody(t *testing.T) {
	item := makeItem("dash-1", "dashboard.grafana.app/v2", "Dash 1", "folder-1", []string{"prod", "ops"}, nil, map[string]any{
		"panels":   panels(2),
		"elements": elements(3),
	})
	item.SetResourceVersion("item-rv")
	item.SetGeneration(7)

	list := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{item}}
	list.SetAPIVersion("dashboard.grafana.app/v2")
	list.SetKind("DashboardList")
	list.SetContinue("next-page")
	list.SetResourceVersion("list-rv")

	data, err := json.Marshal(dashboards.DashboardListSummaryForTest(list))
	if err != nil {
		t.Fatalf("Marshal summary: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal summary: %v", err)
	}

	if got["kind"] != "DashboardSummaryList" {
		t.Fatalf("kind = %v, want DashboardSummaryList", got["kind"])
	}
	metadata := mustMap(t, got["metadata"])
	if metadata["continue"] != "next-page" {
		t.Fatalf("metadata.continue = %v, want next-page", metadata["continue"])
	}

	items := mustSlice(t, got["items"])
	first := mustMap(t, items[0])
	firstMetadata := mustMap(t, first["metadata"])
	if firstMetadata["name"] != "dash-1" {
		t.Fatalf("item metadata.name = %v, want dash-1", firstMetadata["name"])
	}
	if firstMetadata["resourceVersion"] != "item-rv" {
		t.Fatalf("item metadata.resourceVersion = %v, want item-rv", firstMetadata["resourceVersion"])
	}

	spec := mustMap(t, first["spec"])
	if spec["title"] != "Dash 1" {
		t.Fatalf("spec.title = %v, want Dash 1", spec["title"])
	}
	if spec["folder"] != "folder-1" {
		t.Fatalf("spec.folder = %v, want folder-1", spec["folder"])
	}
	if spec["panelCount"] != float64(3) {
		t.Fatalf("spec.panelCount = %v, want 3", spec["panelCount"])
	}
	if _, ok := spec["panels"]; ok {
		t.Fatalf("summary spec unexpectedly includes panels: %v", spec)
	}
	if _, ok := spec["elements"]; ok {
		t.Fatalf("summary spec unexpectedly includes elements: %v", spec)
	}
}

func TestDashboardListOutputValueUsesSummaryForAllListFormats(t *testing.T) {
	list := &unstructured.UnstructuredList{}

	for _, outputFormat := range []string{"table", "wide", "json", "yaml", "agents"} {
		t.Run(outputFormat, func(t *testing.T) {
			data, err := json.Marshal(dashboards.DashboardListOutputValueForTest(list, outputFormat))
			if err != nil {
				t.Fatalf("Marshal output value: %v", err)
			}
			if !jsonContains(data, "DashboardSummaryList") {
				t.Fatalf("structured output value = %s, want DashboardSummaryList", string(data))
			}
		})
	}
}

func jsonContains(data []byte, needle string) bool {
	return json.Valid(data) && strings.Contains(string(data), needle)
}

func mustMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("value type = %T, want map[string]any", v)
	}
	return m
}

func mustSlice(t *testing.T, v any) []any {
	t.Helper()
	s, ok := v.([]any)
	if !ok {
		t.Fatalf("value type = %T, want []any", v)
	}
	return s
}
