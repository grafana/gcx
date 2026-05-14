//nolint:testpackage // tests need access to internal converters and the registry map
package dev

import (
	"testing"

	model "github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestConvertersMapCoversReportedGVKs(t *testing.T) {
	required := []string{
		"Dashboard.dashboard.grafana.app/v0alpha1",
		"Dashboard.dashboard.grafana.app/v1",
		"Dashboard.dashboard.grafana.app/v1beta1",
		"Dashboard.dashboard.grafana.app/v2beta1",
		"Folder.folder.grafana.app/v1",
	}

	for _, key := range required {
		t.Run(key, func(t *testing.T) {
			_, ok := convertersMap[key]
			assert.True(t, ok, "convertersMap missing entry for %s", key)
		})
	}
}

func TestConverters(t *testing.T) {
	tests := []struct {
		name           string
		object         map[string]any
		wantSDKPackage string
		wantContains   string
	}{
		{
			name: "dashboard v1",
			object: map[string]any{
				"apiVersion": "dashboard.grafana.app/v1",
				"kind":       "Dashboard",
				"metadata":   map[string]any{"name": "my-dashboard"},
				"spec": map[string]any{
					"title":         "My Dashboard",
					"schemaVersion": float64(36),
				},
			},
			wantSDKPackage: "dashboard",
			wantContains:   "NewDashboardBuilder",
		},
		{
			name: "folder v1",
			object: map[string]any{
				"apiVersion": "folder.grafana.app/v1",
				"kind":       "Folder",
				"metadata":   map[string]any{"name": "my-folder"},
				"spec": map[string]any{
					"title":       "My Folder",
					"description": "a folder",
				},
			},
			wantSDKPackage: "folderv1beta1",
			wantContains:   "NewFolderBuilder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := model.FromUnstructured(&unstructured.Unstructured{Object: tt.object})
			require.NoError(t, err)

			gvk := res.GroupVersionKind()
			key := gvk.Kind + "." + gvk.GroupVersion().String()

			converter, ok := convertersMap[key]
			require.True(t, ok, "no converter registered for %s", key)

			code, sdkPkg, err := converter(res)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSDKPackage, sdkPkg)
			assert.Contains(t, code, tt.wantContains)
		})
	}
}
