package faro //nolint:testpackage // Exercises the unexported settings wire conversion.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingsWireFormat(t *testing.T) {
	enabled, disabled := true, false
	tests := []struct {
		name     string
		settings *FaroAppSettings
		wire     map[string]string
	}{
		{"nil", nil, map[string]string{}},
		{"empty", &FaroAppSettings{}, map[string]string{}},
		{"enabled with level", &FaroAppSettings{GeolocationEnabled: &enabled, GeolocationLevel: "country"},
			map[string]string{"geolocation.enabled": "1", "geolocation.level": "1"}},
		{"explicit disable", &FaroAppSettings{GeolocationEnabled: &disabled},
			map[string]string{"geolocation.enabled": "0"}},
		{"level only keeps stored enabled flag", &FaroAppSettings{GeolocationLevel: "network"},
			map[string]string{"geolocation.level": "4"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.settings.toAPI()
			require.NoError(t, err)
			assert.Equal(t, tc.wire, got)
			if len(tc.wire) > 0 {
				assert.Equal(t, tc.settings, settingsFromAPI(got))
			}
		})
	}
}

func TestSettingsRejectsUnknownLevel(t *testing.T) {
	_, err := (&FaroAppSettings{GeolocationLevel: "region"}).toAPI()
	require.ErrorContains(t, err, `"region" is invalid`)
}

func TestSettingsFromAPIIgnoresOtherKeys(t *testing.T) {
	assert.Nil(t, settingsFromAPI(map[string]string{"combine_lab_data": "1"}))
	assert.Nil(t, settingsFromAPI(map[string]string{"geolocation.level": "9"}))
}
