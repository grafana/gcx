package dashboards_test

import (
	"bytes"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/dashboards"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func makeItem(name, apiVersion, title, folder string, tags []string, annotations map[string]string, extraSpec map[string]any) unstructured.Unstructured {
	spec := map[string]any{
		"title": title,
	}

	if len(tags) > 0 {
		tagSlice := make([]any, len(tags))
		for i, t := range tags {
			tagSlice[i] = t
		}
		spec["tags"] = tagSlice
	}

	maps.Copy(spec, extraSpec)

	obj := map[string]any{
		"apiVersion": apiVersion,
		"kind":       "Dashboard",
		"metadata": map[string]any{
			"name": name,
		},
		"spec": spec,
	}

	u := unstructured.Unstructured{Object: obj}

	allAnnotations := make(map[string]string)
	if folder != "" {
		allAnnotations["grafana.app/folder"] = folder
	}
	maps.Copy(allAnnotations, annotations)
	if len(allAnnotations) > 0 {
		u.SetAnnotations(allAnnotations)
	}

	// Set a fixed creation timestamp so AGE is deterministic in tests.
	u.SetCreationTimestamp(metav1.Time{Time: time.Now().Add(-1 * time.Hour)})

	return u
}

func panels(n int) any {
	result := make([]any, n)
	for i := range result {
		// Use float64 for the id value: unstructured.NestedSlice deep-copies
		// with DeepCopyJSONValue which only supports JSON-native types (float64,
		// string, bool, map[string]any, []any). Go int is not supported.
		result[i] = map[string]any{"id": float64(i)}
	}
	return result
}

// elements builds a v2-style spec.elements map with n entries (keyed by string id).
func elements(n int) map[string]any {
	m := make(map[string]any, n)
	for i := range n {
		m[fmt.Sprintf("elem-%d", i)] = map[string]any{"type": "panel"}
	}
	return m
}

func TestDashboardTableCodec_Encode_Default(t *testing.T) {
	tests := []struct {
		name        string
		items       []unstructured.Unstructured
		wantHeaders []string
		wantRows    [][]string
	}{
		{
			name:        "empty list",
			items:       nil,
			wantHeaders: []string{"NAME", "TITLE", "FOLDER", "TAGS", "AGE"},
		},
		{
			name: "single dashboard - no tags, no folder",
			items: []unstructured.Unstructured{
				makeItem("my-dash", "dashboard.grafana.app/v1", "My Dashboard", "", nil, nil, nil),
			},
			wantHeaders: []string{"NAME", "TITLE", "FOLDER", "TAGS", "AGE"},
			wantRows: [][]string{
				{"my-dash", "My Dashboard", "General", "", "1h"},
			},
		},
		{
			name: "single dashboard - with tags and folder",
			items: []unstructured.Unstructured{
				makeItem("my-dash", "dashboard.grafana.app/v1", "My Dashboard", "my-folder", []string{"prod", "ops"}, nil, nil),
			},
			wantHeaders: []string{"NAME", "TITLE", "FOLDER", "TAGS", "AGE"},
			wantRows: [][]string{
				{"my-dash", "My Dashboard", "my-folder", "prod, ops", "1h"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := &unstructured.UnstructuredList{Items: tt.items}
			codec := dashboards.NewDashboardTableCodecForTest(false, "")

			var buf bytes.Buffer
			if err := codec.Encode(&buf, list); err != nil {
				t.Fatalf("Encode() error = %v", err)
			}

			output := buf.String()
			for _, h := range tt.wantHeaders {
				if !strings.Contains(output, h) {
					t.Errorf("output missing header %q:\n%s", h, output)
				}
			}
			for _, row := range tt.wantRows {
				for _, cell := range row {
					if cell == "" {
						continue
					}
					if !strings.Contains(output, cell) {
						t.Errorf("output missing cell %q:\n%s", cell, output)
					}
				}
			}

			// Default codec must NOT include PANELS or URL columns
			if strings.Contains(output, "PANELS") {
				t.Errorf("default table must not contain PANELS column:\n%s", output)
			}
			if strings.Contains(output, "URL") {
				t.Errorf("default table must not contain URL column:\n%s", output)
			}
		})
	}
}

func TestDashboardTableCodec_Encode_Wide(t *testing.T) {
	tests := []struct {
		name       string
		items      []unstructured.Unstructured
		grafanaURL string
		wantCols   []string
		notWant    []string
	}{
		{
			name: "v1 panels counted from spec.panels",
			items: []unstructured.Unstructured{
				makeItem("dash1", "dashboard.grafana.app/v1", "Dash 1", "", nil, nil, map[string]any{
					"panels": panels(5),
				}),
			},
			grafanaURL: "https://example.grafana.net",
			wantCols:   []string{"NAME", "TITLE", "FOLDER", "TAGS", "PANELS", "URL", "AGE", "5", "https://example.grafana.net/d/dash1"},
		},
		{
			name: "v2 panels counted from spec.elements map",
			items: []unstructured.Unstructured{
				makeItem("dash2", "dashboard.grafana.app/v2", "Dash 2", "", nil, nil, map[string]any{
					"elements": elements(3),
				}),
			},
			grafanaURL: "https://example.grafana.net",
			wantCols:   []string{"NAME", "TITLE", "PANELS", "3"},
		},
		{
			name: "URL uses slug annotation when present",
			items: []unstructured.Unstructured{
				makeItem("dash3", "dashboard.grafana.app/v1", "Dash 3", "", nil,
					map[string]string{"grafana.app/slug": "my-slug"},
					nil,
				),
			},
			grafanaURL: "https://example.grafana.net",
			wantCols:   []string{"https://example.grafana.net/d/dash3/my-slug"},
		},
		{
			name: "URL empty when grafanaURL not set",
			items: []unstructured.Unstructured{
				makeItem("dash4", "dashboard.grafana.app/v1", "Dash 4", "", nil, nil, nil),
			},
			grafanaURL: "",
			wantCols:   []string{"NAME", "PANELS", "URL", "AGE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := &unstructured.UnstructuredList{Items: tt.items}
			codec := dashboards.NewDashboardTableCodecForTest(true, tt.grafanaURL)

			var buf bytes.Buffer
			if err := codec.Encode(&buf, list); err != nil {
				t.Fatalf("Encode() error = %v", err)
			}

			output := buf.String()
			for _, want := range tt.wantCols {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q:\n%s", want, output)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(output, notWant) {
					t.Errorf("output should not contain %q:\n%s", notWant, output)
				}
			}
		})
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{65 * time.Minute, "1h"},
		{25 * time.Hour, "1d"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := dashboards.FormatAgeForTest(tt.d)
			if got != tt.want {
				t.Errorf("formatAge(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestDashboardTableCodec_Format(t *testing.T) {
	narrow := dashboards.NewDashboardTableCodecForTest(false, "")
	if narrow.Format() != "table" {
		t.Errorf("expected 'table', got %q", narrow.Format())
	}
	wide := dashboards.NewDashboardTableCodecForTest(true, "")
	if wide.Format() != "wide" {
		t.Errorf("expected 'wide', got %q", wide.Format())
	}
}

func TestDashboardTableCodec_Decode(t *testing.T) {
	codec := dashboards.NewDashboardTableCodecForTest(false, "")
	err := codec.Decode(nil, nil)
	if err == nil {
		t.Error("expected error from Decode, got nil")
	}
}
