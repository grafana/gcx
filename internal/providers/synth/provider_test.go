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
	require.Len(t, keys, 3)

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
}

func TestSynthProvider_Validate(t *testing.T) {
	p := &synth.SynthProvider{}

	tests := []struct {
		name    string
		cfg     map[string]string
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: map[string]string{
				"sm-url":   "https://synthetic-monitoring-api.grafana.net",
				"sm-token": "my-token",
			},
		},
		{
			name:    "missing sm-url",
			cfg:     map[string]string{"sm-token": "token"},
			wantErr: true,
		},
		{
			name:    "missing sm-token",
			cfg:     map[string]string{"sm-url": "https://example.com"},
			wantErr: true,
		},
		{
			name:    "empty config",
			cfg:     map[string]string{},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := p.Validate(tc.cfg)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
