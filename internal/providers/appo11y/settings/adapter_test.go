package settings_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/appo11y/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSettings() settings.PluginSettings {
	return settings.PluginSettings{
		JSONData: settings.PluginJSONData{
			DefaultLogQueryMode:       "loki",
			LogsQueryWithNamespace:    "namespace_query",
			LogsQueryWithoutNamespace: "no_namespace_query",
			MetricsMode:               "otel",
		},
	}
}

func TestToResource(t *testing.T) {
	tests := []struct {
		name      string
		settings  settings.PluginSettings
		namespace string
		wantName  string
		wantGroup string
		wantKind  string
	}{
		{
			name:      "full settings",
			settings:  testSettings(),
			namespace: "stack-123",
			wantName:  "default",
			wantGroup: "appo11y.ext.grafana.app",
			wantKind:  "Settings",
		},
		{
			name:      "empty settings",
			settings:  settings.PluginSettings{},
			namespace: "stack-456",
			wantName:  "default",
			wantGroup: "appo11y.ext.grafana.app",
			wantKind:  "Settings",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := settings.ToResource(tc.settings, tc.namespace)
			require.NoError(t, err)

			assert.Equal(t, settings.APIVersion, res.APIVersion())
			assert.Equal(t, tc.wantKind, res.Kind())
			assert.Equal(t, tc.wantName, res.Name())
			assert.Equal(t, tc.namespace, res.Namespace())

			gvk := res.GroupVersionKind()
			assert.Equal(t, tc.wantGroup, gvk.Group)
			assert.Equal(t, "v1alpha1", gvk.Version)
			assert.Equal(t, tc.wantKind, gvk.Kind)
		})
	}
}

func TestToResource_SpecContainsJSONData(t *testing.T) {
	s := testSettings()
	res, err := settings.ToResource(s, "stack-123")
	require.NoError(t, err)

	spec, err := res.Spec()
	require.NoError(t, err)

	specMap, ok := spec.(map[string]any)
	require.True(t, ok, "spec must be a map")

	jsonDataRaw, ok := specMap["jsonData"]
	require.True(t, ok, "spec must contain jsonData")

	jsonData, ok := jsonDataRaw.(map[string]any)
	require.True(t, ok, "jsonData must be a map")

	assert.Equal(t, "loki", jsonData["defaultLogQueryMode"])
	assert.Equal(t, "otel", jsonData["metricsMode"])
	assert.Equal(t, "namespace_query", jsonData["logsQueryWithNamespace"])
	assert.Equal(t, "no_namespace_query", jsonData["logsQueryWithoutNamespace"])
}

func TestFromResource_RoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		settings settings.PluginSettings
	}{
		{
			name:     "full settings",
			settings: testSettings(),
		},
		{
			name: "partial settings",
			settings: settings.PluginSettings{
				JSONData: settings.PluginJSONData{
					DefaultLogQueryMode: "tempo",
				},
			},
		},
		{
			name:     "empty settings",
			settings: settings.PluginSettings{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := settings.ToResource(tc.settings, "stack-123")
			require.NoError(t, err)

			restored, err := settings.FromResource(res)
			require.NoError(t, err)
			require.NotNil(t, restored)

			assert.Equal(t, tc.settings.JSONData.DefaultLogQueryMode, restored.JSONData.DefaultLogQueryMode)
			assert.Equal(t, tc.settings.JSONData.MetricsMode, restored.JSONData.MetricsMode)
			assert.Equal(t, tc.settings.JSONData.LogsQueryWithNamespace, restored.JSONData.LogsQueryWithNamespace)
			assert.Equal(t, tc.settings.JSONData.LogsQueryWithoutNamespace, restored.JSONData.LogsQueryWithoutNamespace)
		})
	}
}

func TestFromResource_MissingSpec(t *testing.T) {
	from := settings.PluginSettings{}
	res, err := settings.ToResource(from, "ns")
	require.NoError(t, err)

	// Manually remove spec from the underlying object
	delete(res.Object.Object, "spec")

	_, err = settings.FromResource(res)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no spec field")
}

func TestToResource_NameIsAlwaysDefault(t *testing.T) {
	s := settings.PluginSettings{
		JSONData: settings.PluginJSONData{MetricsMode: "otel"},
	}
	res, err := settings.ToResource(s, "ns")
	require.NoError(t, err)
	assert.Equal(t, "default", res.Name())
}
