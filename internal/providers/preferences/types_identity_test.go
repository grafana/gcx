package preferences_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/preferences"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
)

// Compile-time assertion that *OrgPreferences satisfies ResourceIdentity.
var _ adapter.ResourceIdentity = &preferences.OrgPreferences{}

func TestOrgPreferences_GetResourceName(t *testing.T) {
	tests := []struct {
		name  string
		prefs preferences.OrgPreferences
	}{
		{
			name:  "zero value",
			prefs: preferences.OrgPreferences{},
		},
		{
			name: "populated",
			prefs: preferences.OrgPreferences{
				Theme:           "dark",
				Timezone:        "UTC",
				WeekStart:       "monday",
				Locale:          "en-US",
				HomeDashboardID: 42,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, "default", tc.prefs.GetResourceName())
		})
	}
}

func TestOrgPreferences_SetResourceName(t *testing.T) {
	original := preferences.OrgPreferences{
		Theme:           "dark",
		Timezone:        "UTC",
		WeekStart:       "monday",
		Locale:          "en-US",
		HomeDashboardID: 42,
	}
	p := original

	p.SetResourceName("something-else")

	// SetResourceName is a no-op — all other fields remain untouched and
	// GetResourceName still returns the singleton name.
	assert.Equal(t, original, p)
	assert.Equal(t, "default", p.GetResourceName())
}

func TestOrgPreferences_RoundTripIdentity(t *testing.T) {
	p := preferences.OrgPreferences{
		Theme:    "dark",
		Timezone: "UTC",
	}

	// Setting any name is ignored; identity always resolves to "default".
	p.SetResourceName("ignored")
	assert.Equal(t, "default", p.GetResourceName())

	// Setting again with a different name has no effect.
	p.SetResourceName("also-ignored")
	assert.Equal(t, "default", p.GetResourceName())
	assert.Equal(t, "dark", p.Theme)
	assert.Equal(t, "UTC", p.Timezone)
}
