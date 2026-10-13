//nolint:testpackage // White-box coverage verifies private identity never enters serialization.
package watcher

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validSpec() map[string]any {
	return map[string]any{"title": "Checkout health", "prompt": "Watch errors", "interval": "15m"}
}

func TestWatcherValidation(t *testing.T) {
	tests := []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"blank title", func(s map[string]any) { s["title"] = "  " }, "title"},
		{"missing prompt", func(s map[string]any) { delete(s, "prompt") }, "prompt"},
		{"unknown spec", func(s map[string]any) { s["status"] = "active" }, "unknown field"},
		{"zero interval", func(s map[string]any) { s["interval"] = "0s" }, "interval"},
		{"negative interval", func(s map[string]any) { s["interval"] = "-5m" }, "interval"},
		{"fractional interval", func(s map[string]any) { s["interval"] = "1.5h" }, "interval"},
		{"subsecond interval", func(s map[string]any) { s["interval"] = "500ms" }, "interval"},
		{"below target range", func(s map[string]any) { s["interval"] = "1s" }, "interval"},
		{"above target range", func(s map[string]any) { s["interval"] = "1d" }, "interval"},
		{"sensitivity enum", func(s map[string]any) { s["sensitivity"] = "maximum" }, "sensitivity"},
		{"timestamp offset", func(s map[string]any) { s["autoStop"] = map[string]any{"at": "2030-01-01T18:00:00"} }, "autoStop.at"},
		{"timestamp date", func(s map[string]any) { s["autoStop"] = map[string]any{"at": "2030-02-30T18:00:00Z"} }, "autoStop.at"},
		{"stop requires time", func(s map[string]any) { s["autoStop"] = map[string]any{"enabled": true} }, "autoStop.at"},
		{"archive requires stop", func(s map[string]any) { s["autoStop"] = map[string]any{"archive": true} }, "autoStop.archive"},
		{"slack requires channel", func(s map[string]any) {
			s["notifications"] = map[string]any{"slack": map[string]any{"enabled": true, "channelId": " "}}
		}, "slack.channelId"},
		{"teams requires channel", func(s map[string]any) { s["notifications"] = map[string]any{"teams": map[string]any{"enabled": true}} }, "teams.channelId"},
		{"severity enum", func(s map[string]any) {
			s["notifications"] = map[string]any{"webhook": map[string]any{"severity": "warning"}}
		}, "webhook.severity"},
		{"unknown nested field", func(s map[string]any) { s["investigation"] = map[string]any{"teams": []string{"example"}} }, "unknown field"},
		{"webhook requires URL", func(s map[string]any) {
			s["notifications"] = map[string]any{"webhook": map[string]any{"enabled": true}}
		}, "webhook.url"},
		{"enabled URL cannot clear", func(s map[string]any) {
			s["notifications"] = map[string]any{"webhook": map[string]any{"enabled": true, "url": map[string]any{"clear": true}}}
		}, "webhook.url"},
		{"null interval", func(s map[string]any) { s["interval"] = nil }, "interval"},
		{"null block", func(s map[string]any) { s["notifications"] = nil }, "notifications"},
		{"null scalar", func(s map[string]any) { s["skipReviewOnCleanRuns"] = nil }, "skipReviewOnCleanRuns"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validSpec()
			tt.edit(input)
			data, err := json.Marshal(input)
			require.NoError(t, err)
			var w Watcher
			err = json.Unmarshal(data, &w)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestSecretInputValidationNeverIncludesValues(t *testing.T) {
	tests := []string{
		`{}`, `{"preserve":false}`, `{"clear":false}`, `{"fromEnv":""}`, `{"fromFile":" "}`,
		`{"preserve":true,"clear":true}`, `{"fromEnv":"DO_NOT_PRINT_ME","preserve":true}`,
		`{"value":"DO_NOT_PRINT_ME"}`, `"DO_NOT_PRINT_ME"`, `{"fromEnv":123}`, `{"fromEnv":"DO_NOT_PRINT_ME","fromEnv":"OTHER_SECRET"}`, `{"clear":true,"clear":true}`, `{"fromFile":null}`,
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			var secret SecretInput
			err := json.Unmarshal([]byte(input), &secret)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "DO_NOT_PRINT_ME")
		})
	}
	for _, input := range []string{`{"fromEnv":"MISSING_VARIABLE"}`, `{"fromFile":"/does-not-exist"}`, `{"preserve":true}`, `{"clear":true}`} {
		var secret SecretInput
		require.NoError(t, json.Unmarshal([]byte(input), &secret))
	}
}

func TestWatcherDefaultsAndCompleteExample(t *testing.T) {
	data, err := json.Marshal(validSpec())
	require.NoError(t, err)
	var minimal Watcher
	require.NoError(t, json.Unmarshal(data, &minimal))
	assert.True(t, minimal.SkipReviewOnCleanRuns)
	assert.Equal(t, "balanced", minimal.Sensitivity)
	assert.Equal(t, "warning-and-critical", minimal.Notifications.Teams.Severity)
	assert.NotNil(t, minimal.DatasourceUIDs)
	assert.NotNil(t, minimal.Investigation.TeamAccess)
	var example struct {
		Spec Watcher `json:"spec"`
	}
	require.NoError(t, json.Unmarshal(WatcherExample(), &example))
	require.NoError(t, example.Spec.Validate())
	assert.Equal(t, "checkout-health", example.Spec.GetResourceName())
	assert.Equal(t, "WATCHER_WEBHOOK_URL", example.Spec.Notifications.Webhook.URL.FromEnv)
}

func TestWatcherIdentityAndPrivateBookkeeping(t *testing.T) {
	w := Watcher{Title: "Checkout / HEALTH!", serverID: "private-id"}
	assert.Equal(t, "checkout-health", w.GetResourceName())
	w.SetResourceName("other")
	assert.Equal(t, "checkout-health", w.GetResourceName())
	data, err := json.Marshal(w)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "private-id")
	assert.Equal(t, "private-id", w.ServerID())
}

func TestWatcherSchema(t *testing.T) {
	var schema map[string]any
	require.NoError(t, json.Unmarshal(WatcherSchema(), &schema))
	properties := testObject(t, schema["properties"])
	spec := testObject(t, properties["spec"])
	assert.Equal(t, false, spec["additionalProperties"])
	fields := testObject(t, spec["properties"])
	expected := []string{"title", "description", "prompt", "datasourceUids", "interval", "sensitivity", "skipReviewOnCleanRuns", "autoStop", "labels", "automaticRecalibration", "notifications", "investigation"}
	assert.Len(t, fields, len(expected))
	for _, key := range expected {
		assert.Contains(t, fields, key)
	}
	notifications := testObject(t, testObject(t, fields["notifications"])["properties"])
	webhook := testObject(t, notifications["webhook"])
	assert.Contains(t, webhook, "allOf")
	secrets := testObject(t, webhook["properties"])
	for _, key := range []string{"url", "bearerToken", "signingSecret"} {
		secret := testObject(t, secrets[key])
		assert.Equal(t, false, secret["additionalProperties"])
		assert.InDelta(t, 1, secret["minProperties"], 0)
		assert.InDelta(t, 1, secret["maxProperties"], 0)
		assert.Len(t, testObject(t, secret["properties"]), 4)
	}
	reflected := Watcher{}.JSONSchema()
	raw, err := json.Marshal(reflected)
	require.NoError(t, err)
	var actual map[string]any
	require.NoError(t, json.Unmarshal(raw, &actual))
	assert.Equal(t, spec, actual)
	var adapted map[string]any
	require.NoError(t, json.Unmarshal(NewTypedCRUDForClient(nil, "").AsAdapter().Schema(), &adapted))
	assert.Equal(t, spec, testObject(t, adapted["properties"])["spec"])
	assert.NotContains(t, strings.ToLower(string(WatcherSchema())), "calibrationcontext")
}

func TestWatcherRejectsDuplicateFields(t *testing.T) {
	var manifest Watcher
	err := json.Unmarshal([]byte(`{"title":"Example","prompt":"Watch errors","sensitivity":"balanced","sensitivity":"relaxed"}`), &manifest)
	require.ErrorContains(t, err, "duplicate field")
}
