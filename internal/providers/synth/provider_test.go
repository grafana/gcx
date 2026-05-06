package synth_test

import (
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
	require.Len(t, keys, 1)

	smDS := keys[0]
	assert.Equal(t, "sm-metrics-datasource-uid", smDS.Name)
	assert.False(t, smDS.Secret)
}

func TestSynthProvider_Validate(t *testing.T) {
	p := &synth.SynthProvider{}

	// Validate always returns nil — the SM datasource UID is resolved from
	// the context's datasources.synth field rather than a provider key.
	tests := []struct {
		name string
		cfg  map[string]string
	}{
		{name: "empty config", cfg: map[string]string{}},
		{name: "irrelevant keys", cfg: map[string]string{"foo": "bar"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, p.Validate(tc.cfg))
		})
	}
}
