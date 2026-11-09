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
		wantErr  string
		oneWay   bool // the wire value does not read back as the input
	}{
		{name: "nil", wire: map[string]string{}},
		{name: "empty", settings: &FaroAppSettings{}, wire: map[string]string{}},
		{name: "enabled with level", settings: &FaroAppSettings{GeolocationEnabled: &enabled, GeolocationLevel: "country"},
			wire: map[string]string{"geolocation.enabled": "1", "geolocation.level": "1"}},
		{name: "explicit disable", settings: &FaroAppSettings{GeolocationEnabled: &disabled},
			wire: map[string]string{"geolocation.enabled": "0"}},
		{name: "level only keeps stored enabled flag", settings: &FaroAppSettings{GeolocationLevel: "network"},
			wire: map[string]string{"geolocation.level": "4"}},
		{name: "sorted country denylist", settings: &FaroAppSettings{GeolocationCountryDenylist: []string{"DE", "FR"}},
			wire: map[string]string{"geolocation.country_denylist": "DE,FR"}},
		{name: "country denylist normalises to the sorted wire list",
			settings: &FaroAppSettings{GeolocationCountryDenylist: []string{"fr", " DE", "FR"}},
			wire:     map[string]string{"geolocation.country_denylist": "DE,FR"}, oneWay: true},
		{name: "empty country denylist clears the stored list",
			settings: &FaroAppSettings{GeolocationCountryDenylist: []string{}},
			wire:     map[string]string{"geolocation.country_denylist": ""}, oneWay: true},
		{name: "rejects an unknown level", settings: &FaroAppSettings{GeolocationLevel: "region"},
			wantErr: `"region" is invalid`},
		{name: "rejects country codes that are not two letters",
			settings: &FaroAppSettings{GeolocationCountryDenylist: []string{"DEU"}}, wantErr: `"DEU" is invalid`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.settings.toAPI()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wire, got)
			if len(tc.wire) > 0 && !tc.oneWay {
				assert.Equal(t, tc.settings, settingsFromAPI(got))
			}
		})
	}
}

func TestSettingsFromAPIIgnoresOtherKeys(t *testing.T) {
	assert.Nil(t, settingsFromAPI(map[string]string{"combineLabData": "1"}))
	assert.Nil(t, settingsFromAPI(map[string]string{"geolocation.level": "9"}))
}

// An app exported with a denylist must keep it when another app is created
// from the export; otherwise the new app enriches sessions the source excluded.
func TestSettingsExportThenCreateKeepsCountryDenylist(t *testing.T) {
	exported := fromAPI(faroAppAPI{
		Name: "source",
		Settings: map[string]string{
			"geolocation.enabled":          "1",
			"geolocation.country_denylist": "DE",
			"combineLabData":               "1",
		},
	})

	created, err := exported.toAPI()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"geolocation.enabled":          "1",
		"geolocation.country_denylist": "DE",
	}, created.Settings)
}
