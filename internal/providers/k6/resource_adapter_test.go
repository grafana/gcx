package k6_test

import (
	"testing"

	"github.com/grafana/grafanactl/internal/providers/k6"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// lookupByGVK searches the global adapter registrations for a matching GVK.
func lookupByGVK(gvk schema.GroupVersionKind) (adapter.Registration, bool) {
	for _, r := range adapter.AllRegistrations() {
		if r.GVK == gvk {
			return r, true
		}
	}
	return adapter.Registration{}, false
}

// TestAllResourcesRegistered verifies that all 5 k6 resource types are
// registered in the global adapter registry via init().
func TestAllResourcesRegistered(t *testing.T) {
	tests := []struct {
		kind   string
		plural string
	}{
		{kind: "Project", plural: "projects"},
		{kind: "LoadTest", plural: "loadtests"},
		{kind: "Schedule", plural: "schedules"},
		{kind: "EnvVar", plural: "envvars"},
		{kind: "LoadZone", plural: "loadzones"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			gvk := schema.GroupVersionKind{
				Group:   "k6.ext.grafana.app",
				Version: "v1alpha1",
				Kind:    tt.kind,
			}
			reg, ok := lookupByGVK(gvk)
			require.True(t, ok, "expected %s to be registered", tt.kind)
			assert.Equal(t, tt.plural, reg.Descriptor.Plural)
			assert.Empty(t, reg.Aliases, "expected no adapter aliases for %s", tt.kind)
		})
	}
}

func TestProjectAdapterRoundTrip(t *testing.T) {
	original := k6.Project{
		ID:               42,
		Name:             "my-project",
		IsDefault:        true,
		GrafanaFolderUID: "abc-123",
		Created:          "2026-01-01T00:00:00Z",
		Updated:          "2026-01-02T00:00:00Z",
	}

	// Project -> Resource
	res, err := k6.ToResource(original, "stack-999")
	require.NoError(t, err)
	assert.Equal(t, "42", res.Raw.GetName())
	assert.Equal(t, "stack-999", res.Raw.GetNamespace())

	// Resource -> Project
	roundTripped, err := k6.FromResource(res)
	require.NoError(t, err)
	assert.Equal(t, original.ID, roundTripped.ID)
	assert.Equal(t, original.Name, roundTripped.Name)
	assert.Equal(t, original.IsDefault, roundTripped.IsDefault)
	assert.Equal(t, original.GrafanaFolderUID, roundTripped.GrafanaFolderUID)
	assert.Equal(t, original.Created, roundTripped.Created)
	assert.Equal(t, original.Updated, roundTripped.Updated)
}

func TestLoadTestAdapterRoundTrip(t *testing.T) {
	original := k6.LoadTest{
		ID:        5,
		Name:      "my-test",
		ProjectID: 42,
		Script:    "export default function() {}",
		Created:   "2026-01-01T00:00:00Z",
	}

	res, err := k6.LoadTestToResource(original, "stack-999")
	require.NoError(t, err)
	assert.Equal(t, "5", res.Raw.GetName())

	roundTripped, err := k6.LoadTestFromResource(res)
	require.NoError(t, err)
	assert.Equal(t, original.ID, roundTripped.ID)
	assert.Equal(t, original.Name, roundTripped.Name)
	assert.Equal(t, original.ProjectID, roundTripped.ProjectID)
	assert.Equal(t, original.Script, roundTripped.Script)
}

func TestScheduleAdapterRoundTrip(t *testing.T) {
	original := k6.Schedule{
		ID:         10,
		LoadTestID: 5,
		Starts:     "2026-06-01T10:00:00Z",
		RecurrenceRule: &k6.RecurrenceRule{
			Frequency: "DAILY",
			Interval:  1,
		},
	}

	res, err := k6.ScheduleToResource(original, "stack-999")
	require.NoError(t, err)
	assert.Equal(t, "10", res.Raw.GetName())

	roundTripped, err := k6.ScheduleFromResource(res)
	require.NoError(t, err)
	assert.Equal(t, original.ID, roundTripped.ID)
	assert.Equal(t, original.LoadTestID, roundTripped.LoadTestID)
	assert.Equal(t, original.Starts, roundTripped.Starts)
	require.NotNil(t, roundTripped.RecurrenceRule)
	assert.Equal(t, "DAILY", roundTripped.RecurrenceRule.Frequency)
	assert.Equal(t, 1, roundTripped.RecurrenceRule.Interval)
}

func TestEnvVarAdapterRoundTrip(t *testing.T) {
	original := k6.EnvVar{
		ID:          3,
		Name:        "MY_VAR",
		Value:       "hello",
		Description: "test var",
	}

	res, err := k6.EnvVarToResource(original, "stack-999")
	require.NoError(t, err)
	assert.Equal(t, "3", res.Raw.GetName())

	roundTripped, err := k6.EnvVarFromResource(res)
	require.NoError(t, err)
	assert.Equal(t, original.ID, roundTripped.ID)
	assert.Equal(t, original.Name, roundTripped.Name)
	assert.Equal(t, original.Value, roundTripped.Value)
	assert.Equal(t, original.Description, roundTripped.Description)
}

func TestLoadZoneAdapterRoundTrip(t *testing.T) {
	original := k6.LoadZone{
		ID:           1,
		Name:         "my-plz",
		K6LoadZoneID: "k6-plz-123",
	}

	res, err := k6.LoadZoneToResource(original, "stack-999")
	require.NoError(t, err)
	// LoadZone uses Name (string) as metadata.name, not the numeric ID.
	assert.Equal(t, "my-plz", res.Raw.GetName())

	roundTripped, err := k6.LoadZoneFromResource(res)
	require.NoError(t, err)
	assert.Equal(t, original.Name, roundTripped.Name)
	assert.Equal(t, original.K6LoadZoneID, roundTripped.K6LoadZoneID)
}

func TestToResource_SetsAPIVersionAndKind(t *testing.T) {
	p := k6.Project{ID: 1, Name: "test"}
	res, err := k6.ToResource(p, "stack-123")
	require.NoError(t, err)

	obj := res.Object.Object
	assert.Equal(t, k6.APIVersion, obj["apiVersion"])
	assert.Equal(t, k6.Kind, obj["kind"])
}

func TestToResource_StripsIDFromSpec(t *testing.T) {
	p := k6.Project{ID: 42, Name: "test"}
	res, err := k6.ToResource(p, "stack-123")
	require.NoError(t, err)

	spec, ok := res.Object.Object["spec"].(map[string]any)
	require.True(t, ok)
	_, hasID := spec["id"]
	assert.False(t, hasID, "spec should not contain the 'id' field (it belongs in metadata.name)")
}
