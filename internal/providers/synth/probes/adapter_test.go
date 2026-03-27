package probes_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToResource(t *testing.T) {
	probe := probes.Probe{
		ID:        1,
		TenantID:  214,
		Name:      "Oregon",
		Region:    "US",
		Public:    true,
		Online:    true,
		Latitude:  45.5,
		Longitude: -122.6,
		Capabilities: probes.ProbeCapabilities{
			DisableScriptedChecks: false,
			DisableBrowserChecks:  true,
		},
	}

	res, err := probes.ToResource(probe, "default")
	require.NoError(t, err)

	assert.Equal(t, "1", res.Raw.GetName())
	assert.Equal(t, "default", res.Raw.GetNamespace())

	obj := res.Object.Object
	assert.Equal(t, probes.APIVersion, obj["apiVersion"])
	assert.Equal(t, probes.Kind, obj["kind"])

	spec, ok := obj["spec"].(map[string]any)
	require.True(t, ok, "spec should be map[string]any")
	assert.Equal(t, "Oregon", spec["name"])
	assert.Equal(t, "US", spec["region"])
	assert.Equal(t, true, spec["public"])

	// Server-managed fields must not appear in spec.
	assert.NotContains(t, spec, "id")
	assert.NotContains(t, spec, "tenantId")
	assert.NotContains(t, spec, "created")
	assert.NotContains(t, spec, "modified")
	assert.NotContains(t, spec, "onlineChange")
	assert.NotContains(t, spec, "online")
	assert.NotContains(t, spec, "version")
}
