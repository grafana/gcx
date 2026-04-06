package adapter_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNaturalKeySpecFieldKey(t *testing.T) {
	tests := []struct {
		name   string
		fields []string
		obj    *unstructured.Unstructured
		want   string
		wantOK bool
	}{
		{
			name:   "single field slugified",
			fields: []string{"name"},
			obj: &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{"name": "My SLO"},
			}},
			want:   "my-slo",
			wantOK: true,
		},
		{
			name:   "multiple fields joined with slash",
			fields: []string{"job", "target"},
			obj: &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{
					"job":    "Web Check",
					"target": "https://example.com",
				},
			}},
			want:   "web-check/https-example-com",
			wantOK: true,
		},
		{
			name:   "missing spec",
			fields: []string{"name"},
			obj: &unstructured.Unstructured{Object: map[string]any{
				"metadata": map[string]any{"name": "test"},
			}},
			want:   "",
			wantOK: false,
		},
		{
			name:   "missing field in spec",
			fields: []string{"name"},
			obj: &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{"other": "value"},
			}},
			want:   "",
			wantOK: false,
		},
		{
			name:   "empty string field",
			fields: []string{"name"},
			obj: &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{"name": ""},
			}},
			want:   "",
			wantOK: false,
		},
		{
			name:   "non-string field",
			fields: []string{"name"},
			obj: &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{"name": 42},
			}},
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := adapter.SpecFieldKey(tt.fields...)
			got, ok := fn(tt.obj)
			require.Equal(t, tt.wantOK, ok, "ok mismatch")
			require.Equal(t, tt.want, got, "key mismatch")
		})
	}
}

func TestNaturalKeyRegistry(t *testing.T) {
	t.Cleanup(adapter.ResetNaturalKeyRegistry)

	gvk1 := schema.GroupVersionKind{Group: "test.example.com", Version: "v1", Kind: "TestResource"}
	gvk2 := schema.GroupVersionKind{Group: "test.example.com", Version: "v1", Kind: "OtherResource"}
	unregistered := schema.GroupVersionKind{Group: "test.example.com", Version: "v1", Kind: "Unknown"}

	t.Run("round-trip register and get", func(t *testing.T) {
		fn := adapter.SpecFieldKey("name")
		adapter.RegisterNaturalKey(gvk1, fn)

		got := adapter.GetNaturalKeyExtractor(gvk1)
		require.NotNil(t, got)

		obj := &unstructured.Unstructured{Object: map[string]any{
			"spec": map[string]any{"name": "Hello World"},
		}}
		key, ok := got(obj)
		require.True(t, ok)
		require.Equal(t, "hello-world", key)
	})

	t.Run("unregistered GVK returns nil", func(t *testing.T) {
		got := adapter.GetNaturalKeyExtractor(unregistered)
		require.Nil(t, got)
	})

	t.Run("multiple GVKs do not interfere", func(t *testing.T) {
		fn1 := adapter.SpecFieldKey("name")
		fn2 := adapter.SpecFieldKey("job", "target")

		adapter.RegisterNaturalKey(gvk1, fn1)
		adapter.RegisterNaturalKey(gvk2, fn2)

		obj := &unstructured.Unstructured{Object: map[string]any{
			"spec": map[string]any{
				"name":   "SLO One",
				"job":    "Web Check",
				"target": "https://grafana.com",
			},
		}}

		got1 := adapter.GetNaturalKeyExtractor(gvk1)
		require.NotNil(t, got1)
		key1, ok1 := got1(obj)
		require.True(t, ok1)
		require.Equal(t, "slo-one", key1)

		got2 := adapter.GetNaturalKeyExtractor(gvk2)
		require.NotNil(t, got2)
		key2, ok2 := got2(obj)
		require.True(t, ok2)
		require.Equal(t, "web-check/https-grafana-com", key2)
	})
}
