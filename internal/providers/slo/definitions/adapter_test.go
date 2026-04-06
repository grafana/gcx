package definitions_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func minimalSlo() definitions.Slo {
	return definitions.Slo{
		UUID:        "test-uuid-123",
		Name:        "My SLO",
		Description: "A test SLO",
		Query: definitions.Query{
			Type: "freeform",
			Freeform: &definitions.FreeformQuery{
				Query: "sum(rate(http_requests_total{status=~\"2..\"}[5m])) / sum(rate(http_requests_total[5m]))",
			},
		},
		Objectives: []definitions.Objective{
			{Value: 0.999, Window: "30d"},
		},
	}
}

func fullSlo() definitions.Slo {
	return definitions.Slo{
		UUID:        "full-uuid-456",
		Name:        "Full SLO",
		Description: "A fully populated SLO",
		Query: definitions.Query{
			Type: "ratio",
			Ratio: &definitions.RatioQuery{
				SuccessMetric: definitions.MetricDef{
					PrometheusMetric: "http_requests_total{status=~\"2..\"}",
					Type:             "counter",
				},
				TotalMetric: definitions.MetricDef{
					PrometheusMetric: "http_requests_total",
					Type:             "counter",
				},
				GroupByLabels: []string{"service"},
			},
		},
		Objectives: []definitions.Objective{
			{Value: 0.999, Window: "30d"},
			{Value: 0.99, Window: "7d"},
		},
		Labels: []definitions.Label{
			{Key: "team", Value: "platform"},
		},
		Alerting: &definitions.Alerting{
			Labels: []definitions.Label{{Key: "severity", Value: "critical"}},
			FastBurn: &definitions.AlertingRule{
				Annotations: []definitions.Label{{Key: "runbook", Value: "https://example.com"}},
				Enrichments: []definitions.Enrichment{{Type: "assistantInvestigation"}},
			},
		},
		DestinationDatasource: &definitions.DestinationDatasource{UID: "prom-uid"},
		Folder:                &definitions.Folder{UID: "folder-uid"},
		SearchExpression:      "team:platform",
	}
}

func TestToResource_MinimalSLO(t *testing.T) {
	slo := minimalSlo()
	res, err := definitions.ToResource(slo, "stack-123")
	require.NoError(t, err)

	assert.Equal(t, definitions.APIVersion, res.APIVersion())
	assert.Equal(t, definitions.Kind, res.Kind())
	assert.Equal(t, "test-uuid-123", res.Name())
	assert.Equal(t, "stack-123", res.Namespace())

	spec, err := res.Spec()
	require.NoError(t, err)
	specMap, ok := spec.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "My SLO", specMap["name"])
	assert.Equal(t, "A test SLO", specMap["description"])
}

func TestToResource_FullSLO(t *testing.T) {
	slo := fullSlo()
	res, err := definitions.ToResource(slo, "stack-456")
	require.NoError(t, err)

	assert.Equal(t, "full-uuid-456", res.Name())

	spec, err := res.Spec()
	require.NoError(t, err)
	specMap, ok := spec.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Full SLO", specMap["name"])
	// Verify nested structures are present
	assert.NotNil(t, specMap["query"])
	assert.NotNil(t, specMap["objectives"])
	assert.NotNil(t, specMap["alerting"])
	assert.NotNil(t, specMap["labels"])
}

func TestToResource_StripsReadOnly(t *testing.T) {
	slo := minimalSlo()
	slo.ReadOnly = &definitions.ReadOnly{
		CreationTimestamp: 1234567890,
		Status:            &definitions.Status{Type: "Ok"},
		Provenance:        "api",
	}

	res, err := definitions.ToResource(slo, "stack-123")
	require.NoError(t, err)

	spec, err := res.Spec()
	require.NoError(t, err)
	specMap, ok := spec.(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, specMap, "readOnly", "readOnly should be stripped from spec")
}

func TestToResource_MapsUUIDToMetadataName(t *testing.T) {
	slo := minimalSlo()
	slo.UUID = "my-custom-uuid"

	res, err := definitions.ToResource(slo, "stack-123")
	require.NoError(t, err)

	assert.Equal(t, "my-custom-uuid", res.Name())

	// UUID should not appear in spec
	spec, err := res.Spec()
	require.NoError(t, err)
	specMap, ok := spec.(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, specMap, "uuid", "uuid should not appear in spec")
}

func TestToResource_SetsCorrectGVK(t *testing.T) {
	slo := minimalSlo()
	res, err := definitions.ToResource(slo, "stack-123")
	require.NoError(t, err)

	gvk := res.GroupVersionKind()
	assert.Equal(t, "slo.ext.grafana.app", gvk.Group)
	assert.Equal(t, "v1alpha1", gvk.Version)
	assert.Equal(t, "SLO", gvk.Kind)
}

func TestFromResource_RestoresUUID(t *testing.T) {
	slo := minimalSlo()
	res, err := definitions.ToResource(slo, "stack-123")
	require.NoError(t, err)

	restored, err := definitions.FromResource(res)
	require.NoError(t, err)

	assert.Equal(t, "test-uuid-123", restored.UUID)
}

func TestRoundTrip_Freeform(t *testing.T) {
	original := minimalSlo()

	res, err := definitions.ToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := definitions.FromResource(res)
	require.NoError(t, err)

	// Zero out ReadOnly on original since it's stripped
	original.ReadOnly = nil

	assert.Equal(t, original.UUID, restored.UUID)
	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.Description, restored.Description)
	assert.Equal(t, original.Query.Type, restored.Query.Type)
	require.NotNil(t, restored.Query.Freeform)
	assert.Equal(t, original.Query.Freeform.Query, restored.Query.Freeform.Query)
	require.Len(t, restored.Objectives, 1)
	assert.InEpsilon(t, original.Objectives[0].Value, restored.Objectives[0].Value, 1e-9)
	assert.Equal(t, original.Objectives[0].Window, restored.Objectives[0].Window)
}

func TestRoundTrip_Ratio(t *testing.T) {
	original := definitions.Slo{
		UUID:        "ratio-uuid",
		Name:        "Ratio SLO",
		Description: "An SLO with ratio query",
		Query: definitions.Query{
			Type: "ratio",
			Ratio: &definitions.RatioQuery{
				SuccessMetric: definitions.MetricDef{
					PrometheusMetric: "http_requests_total{status=~\"2..\"}",
					Type:             "counter",
				},
				TotalMetric: definitions.MetricDef{
					PrometheusMetric: "http_requests_total",
				},
				GroupByLabels: []string{"service", "namespace"},
			},
		},
		Objectives: []definitions.Objective{
			{Value: 0.995, Window: "28d"},
		},
	}

	res, err := definitions.ToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := definitions.FromResource(res)
	require.NoError(t, err)

	assert.Equal(t, original.UUID, restored.UUID)
	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.Query.Type, restored.Query.Type)
	require.NotNil(t, restored.Query.Ratio)
	assert.Equal(t, original.Query.Ratio.SuccessMetric.PrometheusMetric, restored.Query.Ratio.SuccessMetric.PrometheusMetric)
	assert.Equal(t, original.Query.Ratio.TotalMetric.PrometheusMetric, restored.Query.Ratio.TotalMetric.PrometheusMetric)
	assert.Equal(t, original.Query.Ratio.GroupByLabels, restored.Query.Ratio.GroupByLabels)
}

func TestRoundTrip_Threshold(t *testing.T) {
	original := definitions.Slo{
		UUID:        "threshold-uuid",
		Name:        "Threshold SLO",
		Description: "An SLO with threshold query",
		Query: definitions.Query{
			Type: "threshold",
			Threshold: &definitions.ThresholdQuery{
				ThresholdExpression: "sum(rate(http_requests_total[5m]))",
				Threshold: definitions.Threshold{
					Value:    100.0,
					Operator: "gt",
				},
				GroupByLabels: []string{"pod"},
			},
		},
		Objectives: []definitions.Objective{
			{Value: 0.99, Window: "7d"},
		},
	}

	res, err := definitions.ToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := definitions.FromResource(res)
	require.NoError(t, err)

	assert.Equal(t, original.UUID, restored.UUID)
	assert.Equal(t, original.Query.Type, restored.Query.Type)
	require.NotNil(t, restored.Query.Threshold)
	assert.Equal(t, original.Query.Threshold.ThresholdExpression, restored.Query.Threshold.ThresholdExpression)
	assert.InEpsilon(t, original.Query.Threshold.Threshold.Value, restored.Query.Threshold.Threshold.Value, 1e-9)
	assert.Equal(t, original.Query.Threshold.Threshold.Operator, restored.Query.Threshold.Threshold.Operator)
}

func TestRoundTrip_AlertingEnrichments(t *testing.T) {
	original := minimalSlo()
	original.Alerting = &definitions.Alerting{
		FastBurn: &definitions.AlertingRule{
			Annotations: []definitions.Label{{Key: "name", Value: "SLO Burn Rate Very High"}},
			Enrichments: []definitions.Enrichment{{Type: "assistantInvestigation"}},
		},
		SlowBurn: &definitions.AlertingRule{
			Annotations: []definitions.Label{{Key: "name", Value: "SLO Burn Rate High"}},
			Enrichments: []definitions.Enrichment{{Type: "assistantInvestigation"}},
		},
	}

	res, err := definitions.ToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := definitions.FromResource(res)
	require.NoError(t, err)

	require.NotNil(t, restored.Alerting)
	require.NotNil(t, restored.Alerting.FastBurn)
	require.Len(t, restored.Alerting.FastBurn.Enrichments, 1)
	assert.Equal(t, "assistantInvestigation", restored.Alerting.FastBurn.Enrichments[0].Type)

	require.NotNil(t, restored.Alerting.SlowBurn)
	require.Len(t, restored.Alerting.SlowBurn.Enrichments, 1)
	assert.Equal(t, "assistantInvestigation", restored.Alerting.SlowBurn.Enrichments[0].Type)
}

func TestFileNamer(t *testing.T) {
	slo := minimalSlo()
	res, err := definitions.ToResource(slo, "stack-123")
	require.NoError(t, err)

	namer := definitions.FileNamer("yaml")
	path := namer(res)
	assert.Equal(t, "SLO/test-uuid-123.yaml", path)

	namer = definitions.FileNamer("json")
	path = namer(res)
	assert.Equal(t, "SLO/test-uuid-123.json", path)
}
