package faro //nolint:testpackage // Exercises manifest parsing and wire conversion together.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppClassificationManifestRoundTrip(t *testing.T) {
	for _, runtime := range []string{"web-js", "flutter", "react-native", "android-native", "swift-native"} {
		t.Run(runtime, func(t *testing.T) {
			appType := "mobile"
			mobileLabel := "true"
			if runtime == "web-js" {
				appType = "web"
				mobileLabel = "false"
			}
			manifest := `{"apiVersion":"faro.ext.grafana.app/v1alpha1","kind":"FaroApp","spec":{"name":"example-app","appType":"` + appType + `","runtime":"` + runtime + `","extraLogLabels":{"is_mobile":"` + mobileLabel + `","team":"frontend"}}}`
			app, err := readAppFromFile("-", strings.NewReader(manifest))
			require.NoError(t, err)
			wire, err := json.Marshal(app.toAPI())
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(wire, &payload))
			assert.Equal(t, appType, payload["appType"])
			assert.Equal(t, runtime, payload["runtime"])
			var response faroAppAPI
			require.NoError(t, json.Unmarshal(wire, &response))
			result := fromAPI(response)
			assert.Equal(t, appType, result.AppType)
			require.NotNil(t, result.Runtime)
			assert.Equal(t, runtime, *result.Runtime)
			assert.Equal(t, app.ExtraLogLabels, result.ExtraLogLabels)
		})
	}
}

func TestAppRuntimeOmission(t *testing.T) {
	for _, tc := range []struct {
		name, spec string
		present    bool
	}{
		{"omitted", `{"name":"example-app"}`, false},
		{"explicit empty reaches API validation", `{"name":"example-app","runtime":""}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var app FaroApp
			require.NoError(t, json.Unmarshal([]byte(tc.spec), &app))
			wire, err := json.Marshal(app.toAPI())
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(wire, &payload))
			_, present := payload["runtime"]
			assert.Equal(t, tc.present, present)
		})
	}
}
