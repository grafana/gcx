package reports_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/slo/reports"
	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func minimalReport() reports.Report {
	return reports.Report{
		UUID:        "test-uuid-123",
		Name:        "My Report",
		Description: "A test report",
		TimeSpan:    "calendarMonth",
		ReportDefinition: reports.ReportDefinition{
			Slos: []reports.ReportSlo{
				{SloUUID: "slo-uuid-1"},
			},
		},
	}
}

func fullReport() reports.Report {
	return reports.Report{
		UUID:        "full-uuid-456",
		Name:        "Full Report",
		Description: "A fully populated report",
		TimeSpan:    "weeklySundayToSunday",
		Labels: []reports.Label{
			{Key: "team", Value: "platform"},
		},
		ReportDefinition: reports.ReportDefinition{
			Slos: []reports.ReportSlo{
				{SloUUID: "slo-uuid-1"},
				{SloUUID: "slo-uuid-2"},
				{SloUUID: "slo-uuid-3"},
			},
		},
	}
}

func TestToResource_MinimalReport(t *testing.T) {
	report := minimalReport()
	res, err := reportToResource(report, "stack-123")
	require.NoError(t, err)

	assert.Equal(t, "slo.ext.grafana.app/v1alpha1", res.APIVersion())
	assert.Equal(t, "Report", res.Kind())
	assert.Equal(t, "test-uuid-123", res.Name())
	assert.Equal(t, "stack-123", res.Namespace())

	spec, err := res.Spec()
	require.NoError(t, err)
	specMap, ok := spec.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "My Report", specMap["name"])
	assert.Equal(t, "A test report", specMap["description"])
	assert.Equal(t, "calendarMonth", specMap["timeSpan"])
}

func TestToResource_MapsUUIDToMetadataName(t *testing.T) {
	report := minimalReport()
	report.UUID = "my-custom-uuid"

	res, err := reportToResource(report, "stack-123")
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
	report := minimalReport()
	res, err := reportToResource(report, "stack-123")
	require.NoError(t, err)

	gvk := res.GroupVersionKind()
	assert.Equal(t, "slo.ext.grafana.app", gvk.Group)
	assert.Equal(t, "v1alpha1", gvk.Version)
	assert.Equal(t, "Report", gvk.Kind)
}

func TestFromResource_RestoresUUID(t *testing.T) {
	report := minimalReport()
	res, err := reportToResource(report, "stack-123")
	require.NoError(t, err)

	restored, err := reportFromResource(res)
	require.NoError(t, err)

	assert.Equal(t, "test-uuid-123", restored.UUID)
}

func TestRoundTrip_Report(t *testing.T) {
	original := minimalReport()

	res, err := reportToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := reportFromResource(res)
	require.NoError(t, err)

	assert.Equal(t, original.UUID, restored.UUID)
	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.Description, restored.Description)
	assert.Equal(t, original.TimeSpan, restored.TimeSpan)
	require.Len(t, restored.ReportDefinition.Slos, 1)
	assert.Equal(t, original.ReportDefinition.Slos[0].SloUUID, restored.ReportDefinition.Slos[0].SloUUID)
}

func TestRoundTrip_FullReport(t *testing.T) {
	original := fullReport()

	res, err := reportToResource(original, "stack-456")
	require.NoError(t, err)

	restored, err := reportFromResource(res)
	require.NoError(t, err)

	assert.Equal(t, original.UUID, restored.UUID)
	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.TimeSpan, restored.TimeSpan)
	require.Len(t, restored.Labels, 1)
	assert.Equal(t, original.Labels[0].Key, restored.Labels[0].Key)
	assert.Equal(t, original.Labels[0].Value, restored.Labels[0].Value)
	require.Len(t, restored.ReportDefinition.Slos, 3)
}

func reportToResource(report reports.Report, namespace string) (*resources.Resource, error) {
	obj, err := reports.ReportResource().TypedCRUD(nil, namespace).ToUnstructured(report)
	if err != nil {
		return nil, err
	}
	return resources.MustFromObject(obj.Object, resources.SourceInfo{}), nil
}
func reportFromResource(res *resources.Resource) (*reports.Report, error) {
	obj := res.ToUnstructured()
	return reports.ReportResource().TypedCRUD(nil, "").FromUnstructured(&obj)
}
