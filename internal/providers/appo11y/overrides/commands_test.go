package overrides //nolint:testpackage // Tests access unexported overridesTable and parseOverridesFile.

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// overridesTableCodec unit tests (table-driven)
// ---------------------------------------------------------------------------

func TestOverridesTableCodec_Encode_WrongType(t *testing.T) {
	codec := overridesTable().Codec("table")
	err := codec.Encode(io.Discard, "not a MetricsGeneratorConfig")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MetricsGeneratorConfig")
}

func TestOverridesTableCodec_Decode_Unsupported(t *testing.T) {
	codec := overridesTable().Codec("table")
	err := codec.Decode(strings.NewReader(""), nil)
	require.Error(t, err)
}

func TestOverridesTableCodec_Format(t *testing.T) {
	assert.Equal(t, "table", string((overridesTable().Codec("table")).Format()))
	assert.Equal(t, "wide", string((overridesTable().Codec("wide")).Format()))
}

// ---------------------------------------------------------------------------
// parseOverridesFile tests
// ---------------------------------------------------------------------------

func TestParseOverridesFile_YAML(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		ext      string
		wantETag string
		wantErr  bool
	}{
		{
			name: "yaml without ETag annotation",
			content: `
apiVersion: appo11y.ext.grafana.app/v1alpha1
kind: Overrides
metadata:
  name: default
spec:
  metrics_generator:
    disable_collection: false
    collection_interval: "60s"
`,
			ext:      ".yaml",
			wantETag: "",
		},
		{
			name: "yaml with ETag annotation",
			content: `
apiVersion: appo11y.ext.grafana.app/v1alpha1
kind: Overrides
metadata:
  name: default
  annotations:
    appo11y.ext.grafana.app/etag: '"v1-abc123"'
spec:
  metrics_generator:
    disable_collection: true
`,
			ext:      ".yaml",
			wantETag: `"v1-abc123"`,
		},
		{
			name: "json without ETag annotation",
			content: `{
  "apiVersion": "appo11y.ext.grafana.app/v1alpha1",
  "kind": "Overrides",
  "metadata": {"name": "default"},
  "spec": {"metrics_generator": {"disable_collection": false}}
}`,
			ext:      ".json",
			wantETag: "",
		},
		{
			name: "json with ETag annotation",
			content: `{
  "apiVersion": "appo11y.ext.grafana.app/v1alpha1",
  "kind": "Overrides",
  "metadata": {
    "name": "default",
    "annotations": {
      "appo11y.ext.grafana.app/etag": "\"v2-xyz\""
    }
  },
  "spec": {"metrics_generator": {"disable_collection": false}}
}`,
			ext:      ".json",
			wantETag: `"v2-xyz"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write to a temp file.
			f := t.TempDir() + "/overrides" + tt.ext
			writeTestFile(t, f, tt.content)

			typedObj, err := parseOverridesFile(f)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, typedObj)
			assert.Equal(t, "default", typedObj.GetName())
			assert.Equal(t, tt.wantETag, typedObj.Spec.ETag())
		})
	}
}

func TestParseOverridesFile_MissingSpec(t *testing.T) {
	f := t.TempDir() + "/overrides.yaml"
	require.NoError(t, os.WriteFile(f, []byte(`
apiVersion: appo11y.ext.grafana.app/v1alpha1
kind: Overrides
metadata:
  name: default
`), 0600))

	_, err := parseOverridesFile(f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec")
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
}
