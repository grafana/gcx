package checks_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testProbeNames() map[int64]string {
	return map[int64]string{166: "Oregon", 217: "Spain"}
}

func TestToResource(t *testing.T) {
	check := checks.Check{
		ID:        8127,
		TenantID:  214,
		Job:       "grafana-com-health",
		Target:    "https://grafana.com",
		Frequency: 60000,
		Timeout:   10000,
		Enabled:   true,
		Settings:  checks.CheckSettings{"http": map[string]any{"method": "GET"}},
		Probes:    []int64{166, 217},
		Labels:    []checks.Label{{Name: "team", Value: "platform"}},
	}

	res, err := checks.ToResource(check, "default", testProbeNames())
	require.NoError(t, err)

	// metadata.name includes the numeric ID suffix for uniqueness; metadata.uid also carries it.
	assert.Equal(t, "grafana-com-health-8127", res.Raw.GetName())
	assert.Equal(t, "8127", string(res.Raw.GetUID()))
	assert.Equal(t, "default", res.Raw.GetNamespace())

	obj := res.Object.Object
	assert.Equal(t, checks.APIVersion, obj["apiVersion"])
	assert.Equal(t, checks.Kind, obj["kind"])

	spec, ok := obj["spec"].(map[string]any)
	require.True(t, ok, "spec should be map[string]any")

	assert.Equal(t, "grafana-com-health", spec["job"])
	assert.Equal(t, "https://grafana.com", spec["target"])
	assert.InDelta(t, float64(60000), spec["frequency"], 0)
	enabled, ok := spec["enabled"].(bool)
	require.True(t, ok, "enabled should be bool")
	assert.True(t, enabled)

	probeList, ok := spec["probes"].([]any)
	require.True(t, ok, "probes should be []any")
	require.Len(t, probeList, 2)
	assert.Equal(t, "Oregon", probeList[0])
	assert.Equal(t, "Spain", probeList[1])

	// Server-managed fields must not appear in spec.
	assert.NotContains(t, spec, "id")
	assert.NotContains(t, spec, "tenantId")
	assert.NotContains(t, spec, "created")
	assert.NotContains(t, spec, "modified")
}

func TestToResource_UnknownProbeIDFallsBackToNumeric(t *testing.T) {
	check := checks.Check{
		ID:     1,
		Job:    "test",
		Target: "https://test.com",
		Probes: []int64{999},
	}
	res, err := checks.ToResource(check, "default", map[int64]string{})
	require.NoError(t, err)

	spec, ok := res.Object.Object["spec"].(map[string]any)
	require.True(t, ok)
	probeList, ok := spec["probes"].([]any)
	require.True(t, ok)
	assert.Equal(t, "999", probeList[0])
}

func TestFromResource_RoundTrip(t *testing.T) {
	original := checks.Check{
		ID:        8127,
		TenantID:  214,
		Job:       "my-job",
		Target:    "https://target.com",
		Frequency: 60000,
		Timeout:   10000,
		Enabled:   true,
		Settings:  checks.CheckSettings{"ping": map[string]any{"ipVersion": "V4"}},
		Probes:    []int64{166, 217},
	}

	res, err := checks.ToResource(original, "default", testProbeNames())
	require.NoError(t, err)

	spec, id, err := checks.FromResource(res)
	require.NoError(t, err)

	assert.Equal(t, int64(8127), id)
	assert.Equal(t, "my-job", spec.Job)
	assert.Equal(t, "https://target.com", spec.Target)
	assert.Equal(t, int64(60000), spec.Frequency)
	assert.True(t, spec.Enabled)
	assert.Equal(t, []string{"Oregon", "Spain"}, spec.Probes)
}

func TestFromResource_NoUIDReturnsZeroID(t *testing.T) {
	// A resource with no uid (new check, not yet synced to the SM API) should
	// round-trip with id=0 so the push command creates a new check.
	check := checks.Check{
		ID:     0,
		Job:    "new-check",
		Target: "https://new.com",
		Probes: []int64{166},
	}

	res, err := checks.ToResource(check, "default", testProbeNames())
	require.NoError(t, err)

	// ID=0 means no uid is stored.
	assert.Empty(t, res.Raw.GetUID())

	spec, id, err := checks.FromResource(res)
	require.NoError(t, err)

	assert.Equal(t, int64(0), id)
	assert.Equal(t, "new-check", spec.Job)
}

func TestSpecToCheck(t *testing.T) {
	spec := &checks.CheckSpec{
		Job:       "hello",
		Target:    "https://hello.com",
		Frequency: 30000,
		Timeout:   5000,
		Enabled:   true,
		Settings:  checks.CheckSettings{"http": map[string]any{}},
		Probes:    []string{"Oregon"},
	}

	resolvedIDs := []int64{166}
	c := checks.SpecToCheck(spec, 42, 214, resolvedIDs)

	assert.Equal(t, int64(42), c.ID)
	assert.Equal(t, int64(214), c.TenantID)
	assert.Equal(t, "hello", c.Job)
	assert.Equal(t, []int64{166}, c.Probes)
}
