package synth_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSynthProvider_Interface(t *testing.T) {
	var p providers.Provider = &synth.SynthProvider{}
	assert.Equal(t, "synth", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	assert.NotEmpty(t, p.Commands())
}

func TestSynthProvider_ConfigKeys(t *testing.T) {
	p := &synth.SynthProvider{}
	keys := p.ConfigKeys()
	require.Len(t, keys, 4)

	keyMap := make(map[string]providers.ConfigKey)
	for _, k := range keys {
		keyMap[k.Name] = k
	}

	smURL, ok := keyMap["sm-url"]
	require.True(t, ok)
	assert.False(t, smURL.Secret)

	smToken, ok := keyMap["sm-token"]
	require.True(t, ok)
	assert.True(t, smToken.Secret)

	smDS, ok := keyMap["sm-metrics-datasource-uid"]
	require.True(t, ok)
	assert.False(t, smDS.Secret)

	smLogsDS, ok := keyMap["sm-logs-datasource-uid"]
	require.True(t, ok)
	assert.False(t, smLogsDS.Secret)
}

func TestSynthProvider_Validate(t *testing.T) {
	p := &synth.SynthProvider{}

	// Validate always returns nil because both sm-url and sm-token
	// can be auto-discovered at runtime.
	tests := []struct {
		name string
		cfg  map[string]string
	}{
		{name: "both keys set", cfg: map[string]string{"sm-url": "https://example.com", "sm-token": "tok"}},
		{name: "only sm-url", cfg: map[string]string{"sm-url": "https://example.com"}},
		{name: "only sm-token", cfg: map[string]string{"sm-token": "tok"}},
		{name: "empty config", cfg: map[string]string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, p.Validate(tc.cfg))
		})
	}
}

func TestCheckSchemaFolderUID(t *testing.T) {
	p := &synth.SynthProvider{}
	for _, registration := range p.TypedRegistrations() {
		if registration.Descriptor.Kind != "Check" {
			continue
		}
		var schema map[string]any
		require.NotNil(t, registration.Schema, "registration has no Schema")
		require.NoError(t, json.Unmarshal(registration.Schema(), &schema))
		properties, ok := schema["properties"].(map[string]any)
		require.True(t, ok)
		spec, ok := properties["spec"].(map[string]any)
		require.True(t, ok)
		fields, ok := spec["properties"].(map[string]any)
		require.True(t, ok)
		folder, ok := fields["folderUid"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "string", folder["type"])
		assert.NotContains(t, spec["required"], "folderUid")
		assert.NotContains(t, folder, "minLength", "empty string clears the assignment")
		return
	}
	t.Fatal("Check registration not found")
}
