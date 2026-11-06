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
					// The decoder message must render once, not in both the
					// summary and the details.
					assert.Equal(t, 1, strings.Count(err.Error(), s), "rendered error: %s", err.Error())
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
// manifest reader accepts, at every depth, and rejects the rest.
func TestFaroAppSchema_MatchesSpecFields(t *testing.T) {
	var schema map[string]any
	require.NoError(t, json.Unmarshal(FaroAppSchema(), &schema))
	spec := schema["properties"].(map[string]any)["spec"].(map[string]any)

	raw, err := json.Marshal(fullApp())
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))

	assertSchemaCovers(t, "spec", spec, fields)
}

// assertSchemaCovers walks a decoded manifest value alongside its schema node.
// Every object node must forbid extra properties and declare each field present.
func assertSchemaCovers(t *testing.T, path string, node map[string]any, value any) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		props, hasProps := node["properties"].(map[string]any)
		if !hasProps {
			return // free-form map, such as extraLogLabels
		}
		extra, ok := node["additionalProperties"].(bool)
		assert.True(t, ok, "%s must set additionalProperties", path)
		assert.False(t, extra, "%s must set additionalProperties: false", path)
		for name, child := range v {
			childNode, ok := props[name].(map[string]any)
			if !assert.True(t, ok, "%s.%s is missing from the schema", path, name) {
				continue
			}
			assertSchemaCovers(t, path+"."+name, childNode, child)
		}
	case []any:
		items, ok := node["items"].(map[string]any)
		if !ok {
			return
		}
		for _, child := range v {
			assertSchemaCovers(t, path+"[]", items, child)
		}
	}
}
