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
		{"sorted country denylist", &FaroAppSettings{GeolocationCountryDenylist: []string{"DE", "FR"}},
			map[string]string{"geolocation.country_denylist": "DE,FR"}},
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

func TestSettingsCountryDenylist(t *testing.T) {
	t.Run("normalises to the sorted wire list", func(t *testing.T) {
		got, err := (&FaroAppSettings{GeolocationCountryDenylist: []string{"fr", " DE", "FR"}}).toAPI()
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"geolocation.country_denylist": "DE,FR"}, got)
	})

	t.Run("empty list clears the stored list", func(t *testing.T) {
		got, err := (&FaroAppSettings{GeolocationCountryDenylist: []string{}}).toAPI()
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"geolocation.country_denylist": ""}, got)
	})

	t.Run("rejects codes that are not two letters", func(t *testing.T) {
		_, err := (&FaroAppSettings{GeolocationCountryDenylist: []string{"DEU"}}).toAPI()
		require.ErrorContains(t, err, `"DEU" is invalid`)
	})
}

// An app exported with a denylist must keep it when another app is created
// from the export; otherwise the new app enriches sessions the source excluded.
func TestSettingsExportThenCreateKeepsCountryDenylist(t *testing.T) {
	exported := fromAPI(faroAppAPI{
		Name: "source",
		Settings: map[string]string{
			"geolocation.enabled":          "1",
			"geolocation.country_denylist": "DE",
			"combine_lab_data":             "1",
		},
	})

	created, err := exported.toAPI()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"geolocation.enabled":          "1",
		"geolocation.country_denylist": "DE",
	}, created.Settings)
}
