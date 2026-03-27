package reports_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/slo/reports"
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
	res, err := reports.ToResource(report, "stack-123")
	require.NoError(t, err)

	assert.Equal(t, reports.APIVersion, res.APIVersion())
	assert.Equal(t, reports.Kind, res.Kind())
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

	res, err := reports.ToResource(report, "stack-123")
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
	res, err := reports.ToResource(report, "stack-123")
	require.NoError(t, err)

	gvk := res.GroupVersionKind()
	assert.Equal(t, "slo.ext.grafana.app", gvk.Group)
	assert.Equal(t, "v1alpha1", gvk.Version)
	assert.Equal(t, "Report", gvk.Kind)
}

func TestFromResource_RestoresUUID(t *testing.T) {
	report := minimalReport()
	res, err := reports.ToResource(report, "stack-123")
	require.NoError(t, err)

	restored, err := reports.FromResource(res)
	require.NoError(t, err)

	assert.Equal(t, "test-uuid-123", restored.UUID)
}

func TestRoundTrip_Report(t *testing.T) {
	original := minimalReport()

	res, err := reports.ToResource(original, "stack-123")
	require.NoError(t, err)

	restored, err := reports.FromResource(res)
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

	res, err := reports.ToResource(original, "stack-456")
	require.NoError(t, err)

	restored, err := reports.FromResource(res)
	require.NoError(t, err)

	assert.Equal(t, original.UUID, restored.UUID)
	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.TimeSpan, restored.TimeSpan)
	require.Len(t, restored.Labels, 1)
	assert.Equal(t, original.Labels[0].Key, restored.Labels[0].Key)
	assert.Equal(t, original.Labels[0].Value, restored.Labels[0].Value)
	require.Len(t, restored.ReportDefinition.Slos, 3)
}

func TestFileNamer(t *testing.T) {
	report := minimalReport()
	res, err := reports.ToResource(report, "stack-123")
	require.NoError(t, err)

	namer := reports.FileNamer("yaml")
	path := namer(res)
	assert.Equal(t, "Report/test-uuid-123.yaml", path)

	namer = reports.FileNamer("json")
	path = namer(res)
	assert.Equal(t, "Report/test-uuid-123.json", path)
}
