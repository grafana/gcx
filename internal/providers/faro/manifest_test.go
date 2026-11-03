package faro

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestReadAppFromFile(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr []string
		check   func(t *testing.T, app *FaroApp)
	}{
		{
			name: "valid manifest loads",
			input: `
apiVersion: faro.ext.grafana.app/v1alpha1
kind: App
metadata:
  name: my-app-42
spec:
  name: my-app
  corsOrigins:
    - url: https://example.com
  extraLogLabels:
    team: web
  settings:
    geolocationEnabled: true
`,
			check: func(t *testing.T, app *FaroApp) {
				t.Helper()
				assert.Equal(t, "my-app", app.Name)
				assert.Equal(t, "42", app.ID)
				assert.Equal(t, []CORSOrigin{{URL: "https://example.com"}}, app.CORSOrigins)
			},
		},
		{
			name: "misspelled field is rejected and named",
			input: `
apiVersion: faro.ext.grafana.app/v1alpha1
kind: App
metadata:
  name: my-app-42
spec:
  name: my-app
  allowedOrigins:
    - url: https://example.com
`,
			wantErr: []string{`unknown field "allowedOrigins"`, "apps get"},
		},
		{
			name: "unknown nested field is rejected",
			input: `
apiVersion: faro.ext.grafana.app/v1alpha1
kind: App
spec:
  name: my-app
  settings:
    geolocationEnbled: true
`,
			wantErr: []string{`unknown field "geolocationEnbled"`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app, err := readAppFromFile("-", strings.NewReader(tc.input))
			if len(tc.wantErr) > 0 {
				require.Error(t, err)
				for _, s := range tc.wantErr {
					assert.Contains(t, err.Error(), s)
				}
				return
			}
			require.NoError(t, err)
			tc.check(t, app)
		})
	}
}

// TestReadAppFromFile_ExportRoundTrip checks that a manifest carrying every
// FaroApp field, including the read-only ones the API fills in, still loads.
func TestReadAppFromFile_ExportRoundTrip(t *testing.T) {
	runtime := "web"
	enabled := true
	exported := FaroApp{
		ID:                    "42",
		Name:                  "my-app",
		AppType:               "web",
		Runtime:               &runtime,
		AppKey:                "key",
		CollectEndpointURL:    "https://collect.example.com",
		OTLPIngestEndpointURL: "https://otlp.example.com",
		CORSOrigins:           []CORSOrigin{{URL: "https://example.com"}},
		ExtraLogLabels:        map[string]string{"team": "web"},
		Settings: &FaroAppSettings{
			GeolocationEnabled:         &enabled,
			GeolocationLevel:           "city",
			GeolocationCountryDenylist: []string{"US"},
		},
	}
	spec, err := json.Marshal(exported)
	require.NoError(t, err)
	var specMap map[string]any
	require.NoError(t, json.Unmarshal(spec, &specMap))
	manifest, err := yaml.Marshal(map[string]any{
		"apiVersion": "faro.ext.grafana.app/v1alpha1",
		"kind":       "App",
		"metadata":   map[string]any{"name": "my-app-42", "namespace": "default"},
		"spec":       specMap,
	})
	require.NoError(t, err)

	got, err := readAppFromFile("-", strings.NewReader(string(manifest)))
	require.NoError(t, err)
	assert.Equal(t, exported, *got)
}
