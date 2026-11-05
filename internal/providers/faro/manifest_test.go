package faro //nolint:testpackage // Exercises the unexported readAppFromFile manifest loader.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
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
			wantErr: []string{`unknown field "allowedOrigins"`},
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
				var detailed *gcxerrors.DetailedError
				require.ErrorAs(t, err, &detailed)
				require.Error(t, detailed.Parent)
				assert.Contains(t, strings.Join(detailed.Suggestions, "\n"), "gcx frontend apps get <app> -o yaml")
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

// TestReadAppFromFile_AllSpecFields checks that a spec carrying every FaroApp
// field, including the read-only ones the API fills in, still decodes.
func TestReadAppFromFile_AllSpecFields(t *testing.T) {
	exported := fullApp()
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

// fullApp returns an app with every spec field set, as an export would carry.
func fullApp() FaroApp {
	runtime := "web"
	enabled := true
	return FaroApp{
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
}

// TestFaroAppSchema_MatchesSpecFields checks the schema allows every field the
// manifest reader accepts and rejects the rest.
func TestFaroAppSchema_MatchesSpecFields(t *testing.T) {
	var schema struct {
		Properties struct {
			Spec struct {
				AdditionalProperties *bool          `json:"additionalProperties"`
				Properties           map[string]any `json:"properties"`
			} `json:"spec"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(FaroAppSchema(), &schema))
	require.NotNil(t, schema.Properties.Spec.AdditionalProperties)
	assert.False(t, *schema.Properties.Spec.AdditionalProperties)

	raw, err := json.Marshal(fullApp())
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	for name := range fields {
		assert.Contains(t, schema.Properties.Spec.Properties, name)
	}
}
