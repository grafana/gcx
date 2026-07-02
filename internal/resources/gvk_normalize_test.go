package resources_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNormalizeGVK(t *testing.T) {
	t.Cleanup(resources.ResetGVKNormalizers)
	resources.ResetGVKNormalizers()

	canonical := schema.GroupVersionKind{Group: "datasource.grafana.app", Version: "v0alpha1", Kind: "DataSource"}
	const suffix = ".datasource.grafana.app"
	resources.RegisterGVKNormalizer(func(gvk schema.GroupVersionKind) (schema.GroupVersionKind, bool) {
		if gvk.Kind == "DataSource" && gvk.Version == "v0alpha1" &&
			gvk.Group != canonical.Group && len(gvk.Group) > len(suffix) &&
			gvk.Group[len(gvk.Group)-len(suffix):] == suffix {
			return canonical, true
		}
		return schema.GroupVersionKind{}, false
	})

	tests := []struct {
		name string
		in   schema.GroupVersionKind
		want schema.GroupVersionKind
	}{
		{
			name: "per-plugin group collapses to canonical",
			in:   schema.GroupVersionKind{Group: "prometheus.datasource.grafana.app", Version: "v0alpha1", Kind: "DataSource"},
			want: canonical,
		},
		{
			name: "canonical group unchanged",
			in:   canonical,
			want: canonical,
		},
		{
			name: "unrelated group unchanged",
			in:   schema.GroupVersionKind{Group: "dashboard.grafana.app", Version: "v1beta1", Kind: "Dashboard"},
			want: schema.GroupVersionKind{Group: "dashboard.grafana.app", Version: "v1beta1", Kind: "Dashboard"},
		},
		{
			name: "matching suffix but wrong kind unchanged",
			in:   schema.GroupVersionKind{Group: "prometheus.datasource.grafana.app", Version: "v0alpha1", Kind: "Other"},
			want: schema.GroupVersionKind{Group: "prometheus.datasource.grafana.app", Version: "v0alpha1", Kind: "Other"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resources.NormalizeGVK(tt.in))
		})
	}
}

func TestNormalizeGVK_NoNormalizers(t *testing.T) {
	t.Cleanup(resources.ResetGVKNormalizers)
	resources.ResetGVKNormalizers()

	gvk := schema.GroupVersionKind{Group: "g", Version: "v", Kind: "K"}
	assert.Equal(t, gvk, resources.NormalizeGVK(gvk))
}

func TestNormalizeGVK_FirstMatchWins(t *testing.T) {
	t.Cleanup(resources.ResetGVKNormalizers)
	resources.ResetGVKNormalizers()

	first := schema.GroupVersionKind{Group: "first", Version: "v", Kind: "K"}
	second := schema.GroupVersionKind{Group: "second", Version: "v", Kind: "K"}
	resources.RegisterGVKNormalizer(func(_ schema.GroupVersionKind) (schema.GroupVersionKind, bool) {
		return first, true
	})
	resources.RegisterGVKNormalizer(func(_ schema.GroupVersionKind) (schema.GroupVersionKind, bool) {
		return second, true
	})

	assert.Equal(t, first, resources.NormalizeGVK(schema.GroupVersionKind{Group: "x", Version: "v", Kind: "K"}))
}
