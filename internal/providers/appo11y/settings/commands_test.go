package settings //nolint:testpackage // Tests access unexported settingsTableCodec and updateOpts.

// Internal tests for unexported codec types and command wiring.
// Using package settings (not settings_test) to access settingsTableCodec.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingsTableCodec_Format(t *testing.T) {
	tests := []struct {
		name       string
		codec      *settingsTableCodec
		wantFormat string
	}{
		{
			name:       "table format",
			codec:      &settingsTableCodec{},
			wantFormat: "table",
		},
		{
			name:       "wide format",
			codec:      &settingsTableCodec{Wide: true},
			wantFormat: "wide",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantFormat, string(tc.codec.Format()))
		})
	}
}

func TestSettingsTableCodec_Encode_Table(t *testing.T) {
	tests := []struct {
		name         string
		settings     *PluginSettings
		wantContains []string
		notContains  []string
	}{
		{
			name: "full settings",
			settings: &PluginSettings{
				JSONData: PluginJSONData{
					DefaultLogQueryMode:       "loki",
					MetricsMode:               "otel",
					LogsQueryWithNamespace:    "ns_query",
					LogsQueryWithoutNamespace: "no_ns_query",
				},
			},
			wantContains: []string{"NAME", "LOG QUERY MODE", "METRICS MODE", "default", "loki", "otel"},
			notContains:  []string{"LOGS QUERY (NS)", "LOGS QUERY (NO NS)", "ns_query", "no_ns_query"},
		},
		{
			name:         "empty fields show dash",
			settings:     &PluginSettings{},
			wantContains: []string{"NAME", "LOG QUERY MODE", "METRICS MODE", "default", "-"},
			notContains:  []string{"LOGS QUERY (NS)"},
		},
		{
			name: "only log query mode set",
			settings: &PluginSettings{
				JSONData: PluginJSONData{DefaultLogQueryMode: "tempo"},
			},
			wantContains: []string{"default", "tempo", "-"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &settingsTableCodec{}
			var buf bytes.Buffer
			err := codec.Encode(&buf, tc.settings)
			require.NoError(t, err)

			output := buf.String()
			for _, want := range tc.wantContains {
				assert.Contains(t, output, want)
			}
			for _, notWant := range tc.notContains {
				assert.NotContains(t, output, notWant)
			}
		})
	}
}

func TestSettingsTableCodec_Encode_Wide(t *testing.T) {
	tests := []struct {
		name         string
		settings     *PluginSettings
		wantContains []string
	}{
		{
			name: "wide shows all columns",
			settings: &PluginSettings{
				JSONData: PluginJSONData{
					DefaultLogQueryMode:       "loki",
					MetricsMode:               "classic",
					LogsQueryWithNamespace:    "ns_query",
					LogsQueryWithoutNamespace: "no_ns_query",
				},
			},
			wantContains: []string{
				"NAME", "LOG QUERY MODE", "METRICS MODE",
				"LOGS QUERY (NS)", "LOGS QUERY (NO NS)",
				"default", "loki", "classic", "ns_query", "no_ns_query",
			},
		},
		{
			name:     "wide empty fields are blank not dash",
			settings: &PluginSettings{},
			wantContains: []string{
				"NAME", "LOG QUERY MODE", "METRICS MODE",
				"LOGS QUERY (NS)", "LOGS QUERY (NO NS)",
				"default",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &settingsTableCodec{Wide: true}
			var buf bytes.Buffer
			err := codec.Encode(&buf, tc.settings)
			require.NoError(t, err)

			output := buf.String()
			for _, want := range tc.wantContains {
				assert.Contains(t, output, want)
			}
		})
	}
}

func TestSettingsTableCodec_Encode_InvalidType(t *testing.T) {
	codec := &settingsTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not a PluginSettings")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid data type")
}

func TestSettingsTableCodec_Decode_AlwaysErrors(t *testing.T) {
	codec := &settingsTableCodec{}
	err := codec.Decode(strings.NewReader("anything"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support decoding")
}

func TestUpdateOpts_Validate(t *testing.T) {
	tests := []struct {
		name    string
		opts    updateOpts
		wantErr bool
	}{
		{
			name:    "file set",
			opts:    updateOpts{File: "settings.yaml"},
			wantErr: false,
		},
		{
			name:    "file not set",
			opts:    updateOpts{},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCommands_Structure(t *testing.T) {
	cmd := Commands()
	assert.Equal(t, "settings", cmd.Use)

	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	assert.True(t, subcommands["get"], "settings must have a get subcommand")
	assert.True(t, subcommands["update"], "settings must have an update subcommand")
}
