package telemetry

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func fakeGetenv(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

func TestResolveMode(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		configValue string
		want        Mode
	}{
		{
			name: "no env, no config: enabled by default",
			want: ModeEnabled,
		},
		{
			name: "GCX_TELEMETRY=enabled",
			env:  map[string]string{"GCX_TELEMETRY": "enabled"},
			want: ModeEnabled,
		},
		{
			name: "GCX_TELEMETRY=log",
			env:  map[string]string{"GCX_TELEMETRY": "log"},
			want: ModeLog,
		},
		{
			name:        "GCX_TELEMETRY=disabled overrides config enabled",
			env:         map[string]string{"GCX_TELEMETRY": "disabled"},
			configValue: "enabled",
			want:        ModeDisabled,
		},
		{
			name: "GCX_TELEMETRY is case-insensitive",
			env:  map[string]string{"GCX_TELEMETRY": "ENABLED"},
			want: ModeEnabled,
		},
		{
			name:        "unrecognised GCX_TELEMETRY disables",
			env:         map[string]string{"GCX_TELEMETRY": "on"},
			configValue: "enabled",
			want:        ModeDisabled,
		},
		{
			name: "GCX_TELEMETRY=off disables",
			env:  map[string]string{"GCX_TELEMETRY": "off"},
			want: ModeDisabled,
		},
		{
			name: "GCX_TELEMETRY=false disables",
			env:  map[string]string{"GCX_TELEMETRY": "false"},
			want: ModeDisabled,
		},
		{
			name:        "empty GCX_TELEMETRY falls through to config",
			env:         map[string]string{"GCX_TELEMETRY": ""},
			configValue: "log",
			want:        ModeLog,
		},
		{
			name:        "DO_NOT_TRACK=1 overrides config enabled",
			env:         map[string]string{"DO_NOT_TRACK": "1"},
			configValue: "enabled",
			want:        ModeDisabled,
		},
		{
			name: "DO_NOT_TRACK=true disables",
			env:  map[string]string{"DO_NOT_TRACK": "true"},
			want: ModeDisabled,
		},
		{
			name:        "DO_NOT_TRACK=0 is ignored",
			env:         map[string]string{"DO_NOT_TRACK": "0"},
			configValue: "enabled",
			want:        ModeEnabled,
		},
		{
			name: "GCX_TELEMETRY=enabled beats DO_NOT_TRACK=1",
			env: map[string]string{
				"GCX_TELEMETRY": "enabled",
				"DO_NOT_TRACK":  "1",
			},
			want: ModeEnabled,
		},
		{
			name:        "config log mode",
			configValue: "log",
			want:        ModeLog,
		},
		{
			name:        "unrecognised config value disables",
			configValue: "on",
			want:        ModeDisabled,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveMode(fakeGetenv(tc.env), func() string { return tc.configValue })
			assert.Equal(t, tc.want, got)
		})
	}
}

// The Env struct tags are what the docs generator publishes; the package
// reads the constants. If they drift, the docs advertise a variable that
// does nothing while the real one is undocumented.
func TestEnvTagsMatchResolvedNames(t *testing.T) {
	typ := reflect.TypeFor[Env]()
	tags := make(map[string]string, typ.NumField())
	for f := range typ.Fields() {
		tags[f.Name] = f.Tag.Get("env")
	}
	assert.Equal(t, map[string]string{
		"Telemetry":  envTelemetry,
		"DoNotTrack": envDoNotTrack,
		"Endpoint":   envEndpoint,
	}, tags)
}
