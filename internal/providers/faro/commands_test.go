package faro_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/faro"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toTypedObjs(apps []faro.FaroApp) []adapter.TypedObject[faro.FaroApp] {
	objs := make([]adapter.TypedObject[faro.FaroApp], len(apps))
	for i, app := range apps {
		objs[i] = adapter.TypedObject[faro.FaroApp]{Spec: app}
	}
	return objs
}

func TestAppTableCodec_Encode(t *testing.T) {
	tests := []struct {
		name     string
		wide     bool
		apps     []faro.FaroApp
		wantCols []string
		wantRows []string
	}{
		{
			name: "standard columns",
			wide: false,
			apps: []faro.FaroApp{
				{
					ID:                 "42",
					Name:               "my-app",
					AppKey:             "abc123",
					CollectEndpointURL: "https://faro.example.com/collect/abc123",
				},
			},
			wantCols: []string{"NAME", "APP KEY", "COLLECT ENDPOINT URL"},
			wantRows: []string{"my-app-42", "abc123", "https://faro.example.com/collect/abc123"},
		},
		{
			name: "wide columns include extra fields",
			wide: true,
			apps: []faro.FaroApp{
				{
					ID:                 "42",
					Name:               "my-app",
					AppKey:             "abc123",
					CollectEndpointURL: "https://faro.example.com/collect/abc123",
					CORSOrigins:        []faro.CORSOrigin{{URL: "https://app.example.com"}},
					ExtraLogLabels:     map[string]string{"team": "frontend"},
					Settings:           &faro.FaroAppSettings{GeolocationEnabled: true, GeolocationLevel: "country"},
				},
			},
			wantCols: []string{"NAME", "APP KEY", "COLLECT ENDPOINT URL", "CORS ORIGINS", "EXTRA LOG LABELS", "GEOLOCATION"},
			wantRows: []string{"my-app-42", "abc123", "https://app.example.com", "team=frontend", "country"},
		},
		{
			name: "empty fields show dashes",
			wide: true,
			apps: []faro.FaroApp{
				{
					Name: "minimal-app",
				},
			},
			wantRows: []string{"minimal-app", "-"},
		},
		{
			name:     "empty list shows only header",
			wide:     false,
			apps:     []faro.FaroApp{},
			wantCols: []string{"NAME", "APP KEY", "COLLECT ENDPOINT URL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := &faro.AppTableCodec{Wide: tt.wide}
			var buf bytes.Buffer

			err := codec.Encode(&buf, toTypedObjs(tt.apps))
			require.NoError(t, err)

			output := buf.String()
			for _, col := range tt.wantCols {
				assert.Contains(t, output, col, "missing column header %q", col)
			}
			for _, row := range tt.wantRows {
				assert.Contains(t, output, row, "missing row content %q", row)
			}
		})
	}
}

func TestAppTableCodec_Encode_InvalidType(t *testing.T) {
	codec := &faro.AppTableCodec{}
	var buf bytes.Buffer

	err := codec.Encode(&buf, "not a slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []TypedObject[FaroApp]")
}

func TestAppTableCodec_Decode_Unsupported(t *testing.T) {
	codec := &faro.AppTableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support decoding")
}

func TestAppTableCodec_Format(t *testing.T) {
	tests := []struct {
		name string
		wide bool
		want string
	}{
		{name: "standard", wide: false, want: "table"},
		{name: "wide", wide: true, want: "wide"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := &faro.AppTableCodec{Wide: tt.wide}
			assert.Equal(t, tt.want, string(codec.Format()))
		})
	}
}

func TestProviderCommands(t *testing.T) {
	p := &faro.FaroProvider{}
	cmds := p.Commands()

	require.Len(t, cmds, 1, "expected one top-level command")
	faroCmd := cmds[0]
	assert.Equal(t, "frontend", faroCmd.Use)

	// Find the apps subcommand.
	var appsCmd *cobra.Command
	for _, c := range faroCmd.Commands() {
		if c.Use == "apps" {
			appsCmd = c
			break
		}
	}
	require.NotNil(t, appsCmd, "expected apps subcommand")
	assert.Equal(t, []string{"app"}, appsCmd.Aliases)

	// Verify expected subcommands exist.
	subCmds := make(map[string]bool)
	for _, c := range appsCmd.Commands() {
		subCmds[c.Name()] = true
	}

	expectedCmds := []string{"list", "get", "create", "update", "delete",
		"show-sourcemaps", "apply-sourcemap", "remove-sourcemap"}
	for _, name := range expectedCmds {
		assert.True(t, subCmds[name], "missing subcommand %q", name)
	}
}

func TestProviderInterface(t *testing.T) {
	p := &faro.FaroProvider{}

	assert.Equal(t, "faro", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	assert.Len(t, p.ConfigKeys(), 1)
	assert.Equal(t, "faro-api-url", p.ConfigKeys()[0].Name)
	require.NoError(t, p.Validate(nil))

	regs := p.TypedRegistrations()
	require.Len(t, regs, 1)
	assert.NotNil(t, regs[0].Schema)
	assert.NotNil(t, regs[0].Example)
	assert.NotNil(t, regs[0].Factory)
}
