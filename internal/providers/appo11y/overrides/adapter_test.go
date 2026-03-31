package overrides_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/appo11y/overrides"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func minimalConfig() overrides.MetricsGeneratorConfig {
	return overrides.MetricsGeneratorConfig{
		MetricsGenerator: &overrides.MetricsGenerator{
			DisableCollection:  false,
			CollectionInterval: "60s",
		},
	}
}

func fullConfig() overrides.MetricsGeneratorConfig {
	cfg := overrides.MetricsGeneratorConfig{
		MetricsGenerator: &overrides.MetricsGenerator{
			DisableCollection:  true,
			CollectionInterval: "120s",
			Processor: &overrides.Processor{
				ServiceGraphs: &overrides.ServiceGraphs{
					Dimensions: []string{"http.method", "http.status_code"},
				},
				SpanMetrics: &overrides.SpanMetrics{
					Dimensions:       []string{"http.method"},
					EnableTargetInfo: true,
				},
			},
		},
	}
	cfg.SetETag(`"abc123"`)
	return cfg
}

// ---------------------------------------------------------------------------
// ToResource tests
// ---------------------------------------------------------------------------

func TestToResource_SetsCorrectGVK(t *testing.T) {
	cfg := minimalConfig()
	res, err := overrides.ToResource(cfg, "stack-123")
	require.NoError(t, err)

	assert.Equal(t, overrides.APIVersion, res.APIVersion())
	assert.Equal(t, overrides.Kind, res.Kind())
}

func TestToResource_SetsName(t *testing.T) {
	cfg := minimalConfig()
	res, err := overrides.ToResource(cfg, "stack-123")
	require.NoError(t, err)

	assert.Equal(t, "default", res.Name())
}

func TestToResource_SetsNamespace(t *testing.T) {
	cfg := minimalConfig()
	res, err := overrides.ToResource(cfg, "stack-456")
	require.NoError(t, err)

	assert.Equal(t, "stack-456", res.Namespace())
}

func TestToResource_NoETagAnnotationWhenEmpty(t *testing.T) {
	cfg := minimalConfig()
	res, err := overrides.ToResource(cfg, "stack-123")
	require.NoError(t, err)

	annotations := res.Annotations()
	assert.NotContains(t, annotations, overrides.ETagAnnotation)
}

func TestToResource_InjectsETagAnnotation(t *testing.T) {
	cfg := fullConfig()
	res, err := overrides.ToResource(cfg, "stack-123")
	require.NoError(t, err)

	annotations := res.Annotations()
	require.NotNil(t, annotations)
	assert.Equal(t, `"abc123"`, annotations[overrides.ETagAnnotation])
}

func TestToResource_SpecContainsMetricsGenerator(t *testing.T) {
	cfg := minimalConfig()
	res, err := overrides.ToResource(cfg, "stack-123")
	require.NoError(t, err)

	spec, err := res.Spec()
	require.NoError(t, err)

	specMap, ok := spec.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, specMap, "metrics_generator")
}

// ---------------------------------------------------------------------------
// FromResource tests
// ---------------------------------------------------------------------------

func TestFromResource_RestoresSpec(t *testing.T) {
	original := minimalConfig()
	res, err := overrides.ToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := overrides.FromResource(res)
	require.NoError(t, err)

	require.NotNil(t, restored.MetricsGenerator)
	assert.Equal(t, "60s", restored.MetricsGenerator.CollectionInterval)
	assert.False(t, restored.MetricsGenerator.DisableCollection)
}

func TestFromResource_RestoresETag(t *testing.T) {
	original := fullConfig()
	res, err := overrides.ToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := overrides.FromResource(res)
	require.NoError(t, err)

	assert.Equal(t, `"abc123"`, restored.ETag())
}

func TestFromResource_EmptyETagWhenNoAnnotation(t *testing.T) {
	original := minimalConfig()
	res, err := overrides.ToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := overrides.FromResource(res)
	require.NoError(t, err)

	assert.Empty(t, restored.ETag())
}

// ---------------------------------------------------------------------------
// Round-trip tests (table-driven)
// ---------------------------------------------------------------------------

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		cfg       overrides.MetricsGeneratorConfig
		namespace string
		wantETag  string
	}{
		{
			name:      "minimal config without ETag",
			cfg:       minimalConfig(),
			namespace: "stack-123",
			wantETag:  "",
		},
		{
			name:      "full config with ETag",
			cfg:       fullConfig(),
			namespace: "stack-456",
			wantETag:  `"abc123"`,
		},
		{
			name: "disabled collection",
			cfg: overrides.MetricsGeneratorConfig{
				MetricsGenerator: &overrides.MetricsGenerator{
					DisableCollection: true,
				},
			},
			namespace: "stack-789",
			wantETag:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := overrides.ToResource(tt.cfg, tt.namespace)
			require.NoError(t, err)

			restored, err := overrides.FromResource(res)
			require.NoError(t, err)

			assert.Equal(t, tt.wantETag, restored.ETag())
			assert.Equal(t, tt.namespace, res.Namespace())
			assert.Equal(t, "default", res.Name())

			if tt.cfg.MetricsGenerator != nil && restored.MetricsGenerator != nil {
				assert.Equal(t, tt.cfg.MetricsGenerator.DisableCollection, restored.MetricsGenerator.DisableCollection)
				assert.Equal(t, tt.cfg.MetricsGenerator.CollectionInterval, restored.MetricsGenerator.CollectionInterval)
			}
		})
	}
}
