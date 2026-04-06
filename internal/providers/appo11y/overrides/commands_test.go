package overrides //nolint:testpackage // Tests access unexported overridesTableCodec and parseOverridesFile.

import (
	"bytes"
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

func TestOverridesTableCodec_Encode_Columns(t *testing.T) {
	tests := []struct {
		name    string
		cfg     MetricsGeneratorConfig
		wide    bool
		wantIn  []string
		wantOut []string
	}{
		{
			name: "table — enabled collection with interval",
			cfg: MetricsGeneratorConfig{
				MetricsGenerator: &MetricsGenerator{
					DisableCollection:  false,
					CollectionInterval: "60s",
				},
			},
			wide: false,
			wantIn: []string{
				"NAME", "COLLECTION", "INTERVAL", "SERVICE GRAPHS", "SPAN METRICS",
				"default", "enabled", "60s", "disabled", "disabled",
			},
			wantOut: []string{"SG DIMENSIONS", "SM DIMENSIONS"},
		},
		{
			name: "table — disabled collection",
			cfg: MetricsGeneratorConfig{
				MetricsGenerator: &MetricsGenerator{
					DisableCollection:  true,
					CollectionInterval: "30s",
				},
			},
			wide: false,
			wantIn: []string{
				"default", "disabled", "30s",
			},
			wantOut: []string{"SG DIMENSIONS", "SM DIMENSIONS"},
		},
		{
			name: "table — nil MetricsGenerator shows dash for interval",
			cfg:  MetricsGeneratorConfig{},
			wide: false,
			wantIn: []string{
				"NAME", "COLLECTION", "INTERVAL", "SERVICE GRAPHS", "SPAN METRICS",
				"default", "enabled", "-", "disabled", "disabled",
			},
			wantOut: []string{"SG DIMENSIONS", "SM DIMENSIONS"},
		},
		{
			name: "table — service graphs enabled",
			cfg: MetricsGeneratorConfig{
				MetricsGenerator: &MetricsGenerator{
					DisableCollection: false,
					Processor: &Processor{
						ServiceGraphs: &ServiceGraphs{
							Dimensions: []string{"http.method"},
						},
					},
				},
			},
			wide: false,
			wantIn: []string{
				"default", "enabled", "enabled", "disabled",
			},
			wantOut: []string{"SG DIMENSIONS", "SM DIMENSIONS"},
		},
		{
			name: "wide — shows SG DIMENSIONS and SM DIMENSIONS columns",
			cfg: MetricsGeneratorConfig{
				MetricsGenerator: &MetricsGenerator{
					DisableCollection:  false,
					CollectionInterval: "60s",
					Processor: &Processor{
						ServiceGraphs: &ServiceGraphs{
							Dimensions: []string{"http.method", "db.name"},
						},
						SpanMetrics: &SpanMetrics{
							Dimensions: []string{"service.name"},
						},
					},
				},
			},
			wide: true,
			wantIn: []string{
				"NAME", "COLLECTION", "INTERVAL", "SERVICE GRAPHS", "SPAN METRICS",
				"SG DIMENSIONS", "SM DIMENSIONS",
				"default", "enabled", "60s", "enabled", "enabled",
				"http.method, db.name", "service.name",
			},
			wantOut: []string{},
		},
		{
			name: "wide — no dimensions shows empty",
			cfg: MetricsGeneratorConfig{
				MetricsGenerator: &MetricsGenerator{
					Processor: &Processor{
						ServiceGraphs: &ServiceGraphs{},
						SpanMetrics:   &SpanMetrics{},
					},
				},
			},
			wide: true,
			wantIn: []string{
				"SG DIMENSIONS", "SM DIMENSIONS",
				"enabled", "enabled",
			},
			wantOut: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := &overridesTableCodec{Wide: tt.wide}
			var buf bytes.Buffer

			err := codec.Encode(&buf, tt.cfg)
			require.NoError(t, err)

			output := buf.String()
			for _, want := range tt.wantIn {
				assert.Contains(t, output, want, "expected %q in output:\n%s", want, output)
			}
			for _, notWant := range tt.wantOut {
				assert.NotContains(t, output, notWant, "expected %q NOT in output:\n%s", notWant, output)
			}
		})
	}
}

func TestOverridesTableCodec_Encode_WrongType(t *testing.T) {
	codec := &overridesTableCodec{}
	err := codec.Encode(io.Discard, "not a MetricsGeneratorConfig")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MetricsGeneratorConfig")
}

func TestOverridesTableCodec_Decode_Unsupported(t *testing.T) {
	codec := &overridesTableCodec{}
	err := codec.Decode(strings.NewReader(""), nil)
	require.Error(t, err)
}

func TestOverridesTableCodec_Format(t *testing.T) {
	assert.Equal(t, "table", string((&overridesTableCodec{Wide: false}).Format()))
	assert.Equal(t, "wide", string((&overridesTableCodec{Wide: true}).Format()))
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
