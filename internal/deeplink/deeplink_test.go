package deeplink_test

import (
	"testing"

	"github.com/grafana/gcx/internal/deeplink"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResolve(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "test.grafana.app", Version: "v1", Kind: "Widget"}
	deeplink.RegisterPattern(gvk, "/a/test-app/widgets/{name}")

	tests := []struct {
		name     string
		host     string
		gvk      schema.GroupVersionKind
		resName  string
		expected string
	}{
		{
			name:     "basic resolution",
			host:     "https://mystack.grafana.net",
			gvk:      gvk,
			resName:  "widget-1",
			expected: "https://mystack.grafana.net/a/test-app/widgets/widget-1",
		},
		{
			name:     "host with trailing slash",
			host:     "https://mystack.grafana.net/",
			gvk:      gvk,
			resName:  "widget-1",
			expected: "https://mystack.grafana.net/a/test-app/widgets/widget-1",
		},
		{
			name:     "unknown GVK returns empty",
			host:     "https://mystack.grafana.net",
			gvk:      schema.GroupVersionKind{Group: "unknown", Version: "v1", Kind: "Unknown"},
			resName:  "foo",
			expected: "",
		},
		{
			name:     "empty name",
			host:     "https://mystack.grafana.net",
			gvk:      gvk,
			resName:  "",
			expected: "https://mystack.grafana.net/a/test-app/widgets/",
		},
		{
			name:     "version-agnostic: matches different version",
			host:     "https://mystack.grafana.net",
			gvk:      schema.GroupVersionKind{Group: "test.grafana.app", Version: "v2beta1", Kind: "Widget"},
			resName:  "widget-1",
			expected: "https://mystack.grafana.net/a/test-app/widgets/widget-1",
		},
		{
			name:     "dashboard v0alpha1",
			host:     "https://mystack.grafana.net",
			gvk:      schema.GroupVersionKind{Group: "dashboard.grafana.app", Version: "v0alpha1", Kind: "Dashboard"},
			resName:  "abc-123",
			expected: "https://mystack.grafana.net/d/abc-123",
		},
		{
			name:     "dashboard v1beta1",
			host:     "https://mystack.grafana.net",
			gvk:      schema.GroupVersionKind{Group: "dashboard.grafana.app", Version: "v1beta1", Kind: "Dashboard"},
			resName:  "abc-123",
			expected: "https://mystack.grafana.net/d/abc-123",
		},
		{
			name:     "folder any version",
			host:     "https://mystack.grafana.net",
			gvk:      schema.GroupVersionKind{Group: "folder.grafana.app", Version: "v99", Kind: "Folder"},
			resName:  "my-folder",
			expected: "https://mystack.grafana.net/dashboards/f/my-folder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deeplink.Resolve(tt.host, tt.gvk, tt.resName)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestInjectURL(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "test.grafana.app", Version: "v1", Kind: "Gadget"}
	deeplink.RegisterPattern(gvk, "/a/test-app/gadgets/{name}")

	t.Run("injects url for known GVK", func(t *testing.T) {
		obj := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "test.grafana.app/v1",
			"kind":       "Gadget",
			"metadata":   map[string]any{"name": "gadget-42"},
		}}
		deeplink.InjectURL(obj, "https://mystack.grafana.net")
		assert.Equal(t, "https://mystack.grafana.net/a/test-app/gadgets/gadget-42", obj.Object["url"])
	})

	t.Run("no-op for unknown GVK", func(t *testing.T) {
		obj := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "unknown/v1",
			"kind":       "Unknown",
			"metadata":   map[string]any{"name": "thing"},
		}}
		deeplink.InjectURL(obj, "https://mystack.grafana.net")
		_, exists := obj.Object["url"]
		assert.False(t, exists)
	})
}

func TestInjectURLs(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "batch.grafana.app", Version: "v1", Kind: "Job"}
	deeplink.RegisterPattern(gvk, "/a/batch-app/jobs/{name}")

	items := []unstructured.Unstructured{
		{Object: map[string]any{
			"apiVersion": "batch.grafana.app/v1",
			"kind":       "Job",
			"metadata":   map[string]any{"name": "job-1"},
		}},
		{Object: map[string]any{
			"apiVersion": "unknown/v1",
			"kind":       "Unknown",
			"metadata":   map[string]any{"name": "nope"},
		}},
		{Object: map[string]any{
			"apiVersion": "batch.grafana.app/v1",
			"kind":       "Job",
			"metadata":   map[string]any{"name": "job-2"},
		}},
	}

	deeplink.InjectURLs(items, "https://mystack.grafana.net")

	assert.Equal(t, "https://mystack.grafana.net/a/batch-app/jobs/job-1", items[0].Object["url"])
	_, exists := items[1].Object["url"]
	assert.False(t, exists)
	assert.Equal(t, "https://mystack.grafana.net/a/batch-app/jobs/job-2", items[2].Object["url"])
}
