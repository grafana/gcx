package datasources_test

import (
	"encoding/json"
	"testing"

	dsclient "github.com/grafana/gcx/internal/datasources"
	provds "github.com/grafana/gcx/internal/providers/datasources"
	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestStaticDescriptor(t *testing.T) {
	desc := provds.StaticDescriptor()
	assert.Equal(t, "datasource.grafana.app", desc.GroupVersion.Group)
	assert.Equal(t, "v0alpha1", desc.GroupVersion.Version)
	assert.Equal(t, "DataSource", desc.Kind)
	assert.Equal(t, "datasources", desc.Plural)
}

func TestProviderTypedRegistrations(t *testing.T) {
	regs := (&provds.Provider{}).TypedRegistrations()
	require.Len(t, regs, 1)

	reg := regs[0]
	assert.Equal(t, provds.StaticDescriptor().GroupVersionKind(), reg.GVK)
	// Every registration must carry a non-nil Schema, and a non-nil Example for
	// writable resources.
	require.NotNil(t, reg.Schema)
	require.NotNil(t, reg.Example)
	require.NotNil(t, reg.Factory)
}

// TestDatasourceGVKNormalization verifies the normalizer registered in the
// package init() collapses Grafana's per-plugin datasource groups onto the
// single canonical descriptor, while leaving everything else untouched.
func TestDatasourceGVKNormalization(t *testing.T) {
	canonical := provds.StaticDescriptor().GroupVersionKind()

	tests := []struct {
		name string
		in   schema.GroupVersionKind
		want schema.GroupVersionKind
	}{
		{
			name: "core plugin per-plugin group collapses",
			in:   schema.GroupVersionKind{Group: "prometheus.datasource.grafana.app", Version: "v0alpha1", Kind: "DataSource"},
			want: canonical,
		},
		{
			name: "dashed plugin id per-plugin group collapses",
			in:   schema.GroupVersionKind{Group: "yesoreyeram-infinity-datasource.datasource.grafana.app", Version: "v0alpha1", Kind: "DataSource"},
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
			name: "datasource group but wrong version unchanged",
			in:   schema.GroupVersionKind{Group: "prometheus.datasource.grafana.app", Version: "v1", Kind: "DataSource"},
			want: schema.GroupVersionKind{Group: "prometheus.datasource.grafana.app", Version: "v1", Kind: "DataSource"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resources.NormalizeGVK(tt.in))
		})
	}
}

func TestDatasourceSchemaIsValidEnvelope(t *testing.T) {
	var schema map[string]any
	require.NoError(t, json.Unmarshal(provds.DatasourceSchema(), &schema))

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	// The converged envelope adds a top-level `secure` block alongside the
	// standard envelope fields.
	for _, key := range []string{"apiVersion", "kind", "metadata", "spec", "secure"} {
		assert.Contains(t, props, key)
	}
	// apiVersion accepts per-plugin and canonical groups (pattern, not const).
	apiVersion, ok := props["apiVersion"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, apiVersion, "pattern")
	assert.NotContains(t, apiVersion, "const")
}

func TestDatasourceExampleIsValidManifest(t *testing.T) {
	var example map[string]any
	require.NoError(t, json.Unmarshal(provds.DatasourceExample(), &example))

	// The example renders the converged shape: per-plugin apiVersion, kind, a
	// top-level secure block, and a title-keyed spec.
	assert.Equal(t, "prometheus.datasource.grafana.app/v0alpha1", example["apiVersion"])
	assert.Equal(t, "DataSource", example["kind"])
	assert.Contains(t, example, "secure")

	spec, err := json.Marshal(example["spec"])
	require.NoError(t, err)
	var ms dsclient.DataSourceSpec
	require.NoError(t, json.Unmarshal(spec, &ms))
	assert.Equal(t, "prometheus", ms.Type)
	assert.Equal(t, "My Prometheus", ms.Title)
}

func TestDatasourceToUnstructured(t *testing.T) {
	ds := &dsclient.Datasource{
		UID:              "abc",
		Name:             "My Prom",
		Type:             "prometheus",
		Access:           "proxy",
		URL:              "http://p",
		BasicAuth:        true,
		JSONData:         map[string]any{"httpMethod": "POST"},
		SecureJSONFields: map[string]bool{"basicAuthPassword": true},
	}

	u, err := provds.DatasourceToUnstructured(ds, "default")
	require.NoError(t, err)

	assert.Equal(t, "prometheus.datasource.grafana.app/v0alpha1", u.GetAPIVersion())
	assert.Equal(t, "DataSource", u.GetKind())
	assert.Equal(t, "abc", u.GetName())
	assert.Equal(t, "default", u.GetNamespace())

	spec, ok := u.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "My Prom", spec["title"])
	assert.Equal(t, "prometheus", spec["type"])
	assert.NotContains(t, spec, "name") // display name moved to title

	secure, ok := u.Object["secure"].(map[string]any)
	require.True(t, ok)
	bap, ok := secure["basicAuthPassword"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "basicAuthPassword", bap["name"]) // read-back reference
	assert.NotContains(t, bap, "create")              // values are never returned
}

func TestUnstructuredToDatasource(t *testing.T) {
	t.Run("per-plugin apiVersion with inline secret", func(t *testing.T) {
		raw := `{
		  "apiVersion":"prometheus.datasource.grafana.app/v0alpha1",
		  "kind":"DataSource",
		  "metadata":{"name":"abc"},
		  "secure":{"basicAuthPassword":{"create":"s3cr3t"}},
		  "spec":{"type":"prometheus","title":"My Prom","access":"proxy","url":"http://p","basicAuth":true}
		}`
		var obj map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &obj))

		ds, err := provds.UnstructuredToDatasource(&unstructured.Unstructured{Object: obj})
		require.NoError(t, err)
		assert.Equal(t, "abc", ds.UID)
		assert.Equal(t, "My Prom", ds.Name)
		assert.Equal(t, "prometheus", ds.Type)
		assert.Equal(t, "s3cr3t", ds.SecureJSONData["basicAuthPassword"])
	})

	t.Run("canonical apiVersion without spec.type is rejected", func(t *testing.T) {
		raw := `{"apiVersion":"datasource.grafana.app/v0alpha1","kind":"DataSource","metadata":{"name":"x"},"spec":{"title":"t"}}`
		var obj map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &obj))

		_, err := provds.UnstructuredToDatasource(&unstructured.Unstructured{Object: obj})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "spec.type is required")
	})
}
