//nolint:testpackage // White-box coverage verifies private identity never enters serialization.
package watcher

import (
	"encoding/json"
	"fmt"
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
		{"sensitivity enum", func(s map[string]any) { s["sensitivity"] = " " }, "sensitivity"},
		{"timestamp offset", func(s map[string]any) { s["autoStop"] = map[string]any{"at": "2030-01-01T18:00:00"} }, "autoStop.at"},
		{"timestamp date", func(s map[string]any) { s["autoStop"] = map[string]any{"at": "2030-02-30T18:00:00Z"} }, "autoStop.at"},
		{"stop requires time", func(s map[string]any) { s["autoStop"] = map[string]any{"enabled": true} }, "autoStop.at"},
		{"archive requires stop", func(s map[string]any) { s["autoStop"] = map[string]any{"archive": true} }, "autoStop.archive"},
		{"slack requires channel", func(s map[string]any) {
			s["notifications"] = map[string]any{"slack": map[string]any{"enabled": true, "channelId": " "}}
		}, "slack.channelId"},
		{"teams requires channel", func(s map[string]any) { s["notifications"] = map[string]any{"teams": map[string]any{"enabled": true}} }, "teams.channelId"},
		{"severity enum", func(s map[string]any) {
			s["notifications"] = map[string]any{"webhook": map[string]any{"severity": " "}}
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
	assert.Equal(t, SlackNotification{TargetType: "channel", Severity: "warning-and-critical"}, minimal.Notifications.Slack)
	assert.Equal(t, "warning-and-critical", minimal.Notifications.Teams.Severity)
	assert.Equal(t, AlertingNotification{Severity: "warning-and-critical"}, minimal.Notifications.Alerting)
	assert.NotNil(t, minimal.DatasourceUIDs)
	assert.NotNil(t, minimal.Investigation.TeamAccess)
	var example struct {
		Spec Watcher `json:"spec"`
	}
	require.NoError(t, json.Unmarshal(WatcherExample(), &example))
	require.NoError(t, example.Spec.Validate())
	assert.Equal(t, "checkout-health", example.Spec.GetResourceName())
	assert.Equal(t, "WATCHER_WEBHOOK_URL", example.Spec.Notifications.Webhook.URL.FromEnv)
	assert.Equal(t, SlackNotification{TargetType: "dm", UserID: "U_EXAMPLE", TeamID: "T_EXAMPLE", Severity: "warning-and-critical"}, example.Spec.Notifications.Slack)
	assert.Equal(t, AlertingNotification{Severity: "critical", RoutingTree: "example-policy-tree"}, example.Spec.Notifications.Alerting)
}

func TestWatcherNotificationTargets(t *testing.T) {
	tests := []struct {
		name          string
		notifications string
		wantError     string
	}{
		{"omitted blocks", `{}`, ""},
		{"disabled empty channel", `{"slack":{"enabled":false,"targetType":"channel","channelId":""}}`, ""},
		{"legacy channel", `{"slack":{"enabled":true,"channelId":"C_EXAMPLE"}}`, ""},
		{"enabled channel missing ID", `{"slack":{"enabled":true}}`, "slack.channelId"},
		{"disabled channel blank ID", `{"slack":{"channelId":" "}}`, "slack.channelId"},
		{"disabled channel workspace only", `{"slack":{"teamId":"T_EXAMPLE"}}`, ""},
		{"disabled channel user only", `{"slack":{"userId":"U_EXAMPLE"}}`, ""},
		{"enabled channel workspace only", `{"slack":{"enabled":true,"teamId":"T_EXAMPLE"}}`, "slack.channelId"},
		{"enabled channel user only", `{"slack":{"enabled":true,"userId":"U_EXAMPLE"}}`, "slack.channelId"},
		{"disabled DM missing ID", `{"slack":{"targetType":"dm"}}`, "slack.userId"},
		{"enabled DM blank ID", `{"slack":{"enabled":true,"targetType":"dm","userId":" "}}`, "slack.userId"},
		{"disabled DM", `{"slack":{"targetType":"dm","userId":"U_EXAMPLE"}}`, ""},
		{"enabled DM", `{"slack":{"enabled":true,"targetType":"dm","userId":"U_EXAMPLE","teamId":"T_EXAMPLE"}}`, ""},
		{"future target", `{"slack":{"enabled":true,"targetType":"future-target","severity":"future-severity"}}`, ""},
		{"blank target type", `{"slack":{"targetType":" "}}`, "slack.targetType"},
		{"blank slack severity", `{"slack":{"severity":" "}}`, "slack.severity"},
		{"blank alerting severity", `{"alerting":{"severity":" "}}`, "alerting.severity"},
		{"future alerting severity", `{"alerting":{"enabled":true,"severity":"future-severity","routingTree":""}}`, ""},
		{"disabled named route", `{"alerting":{"routingTree":"example-policy-tree"}}`, ""},
		{"Teams rejects target type", `{"teams":{"targetType":"channel"}}`, "unknown field"},
		{"Teams rejects user ID", `{"teams":{"userId":"U_EXAMPLE"}}`, "unknown field"},
		{"Teams rejects workspace ID", `{"teams":{"teamId":"T_EXAMPLE"}}`, "unknown field"},
		{"null Slack block", `{"slack":null}`, "slack must not be null"},
		{"null Alerting block", `{"alerting":null}`, "alerting must not be null"},
		{"scalar Slack block", `{"slack":false}`, "cannot unmarshal"},
		{"array Alerting block", `{"alerting":[]}`, "cannot unmarshal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w Watcher
			err := json.Unmarshal([]byte(`{"title":"Example","prompt":"Watch errors","notifications":`+tt.notifications+`}`), &w)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			require.NoError(t, w.Validate())
		})
	}
}

func TestWatcherNotificationRoundTrip(t *testing.T) {
	for _, targetType := range []string{"channel", "dm", "future-target"} {
		t.Run(targetType, func(t *testing.T) {
			input := validSpec()
			slack := SlackNotification{Enabled: false, TargetType: targetType, ChannelID: "C_EXAMPLE", UserID: "U_EXAMPLE", TeamID: "T_EXAMPLE", Severity: "future-severity"}
			alerting := AlertingNotification{Enabled: false, Severity: "future-severity", RoutingTree: " legacy-route "}
			input["notifications"] = map[string]any{"slack": slack, "alerting": alerting}
			data, err := json.Marshal(input)
			require.NoError(t, err)
			var w Watcher
			require.NoError(t, json.Unmarshal(data, &w))
			data, err = json.Marshal(w)
			require.NoError(t, err)
			var roundtrip Watcher
			require.NoError(t, json.Unmarshal(data, &roundtrip))
			assert.Equal(t, slack, roundtrip.Notifications.Slack)
			assert.Equal(t, alerting, roundtrip.Notifications.Alerting)
		})
	}
}

func TestWatcherNotificationFieldShapes(t *testing.T) {
	for _, field := range []struct{ block, name string }{
		{"slack", "targetType"}, {"slack", "channelId"}, {"slack", "userId"}, {"slack", "teamId"},
		{"slack", "severity"}, {"alerting", "severity"}, {"alerting", "routingTree"},
	} {
		for _, value := range []any{nil, true, 123, []string{}, map[string]any{}} {
			t.Run(field.block+"."+field.name+"/"+fmt.Sprintf("%T", value), func(t *testing.T) {
				input := validSpec()
				input["notifications"] = map[string]any{field.block: map[string]any{field.name: value}}
				data, err := json.Marshal(input)
				require.NoError(t, err)
				var w Watcher
				require.Error(t, json.Unmarshal(data, &w))
			})
		}
	}
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
	for _, tt := range []struct {
		name     string
		fields   []string
		defaults map[string]string
	}{
		{"slack", []string{"enabled", "targetType", "channelId", "userId", "teamId", "severity"}, map[string]string{"targetType": "channel", "severity": "warning-and-critical"}},
		{"teams", []string{"enabled", "channelId", "severity"}, map[string]string{"severity": "warning-and-critical"}},
		{"alerting", []string{"enabled", "severity", "routingTree"}, map[string]string{"severity": "warning-and-critical", "routingTree": ""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			block := testObject(t, notifications[tt.name])
			assert.Equal(t, false, block["additionalProperties"])
			properties := testObject(t, block["properties"])
			assert.Len(t, properties, len(tt.fields))
			for _, field := range tt.fields {
				assert.Contains(t, properties, field)
				if field != "enabled" {
					assert.Equal(t, "string", testObject(t, properties[field])["type"])
				}
			}
			for field, value := range tt.defaults {
				assert.Equal(t, value, testObject(t, properties[field])["default"])
			}
			assert.Equal(t, `\S`, testObject(t, properties["severity"])["pattern"])
		})
	}
	assert.Equal(t, `\S`, testObject(t, testObject(t, testObject(t, notifications["slack"])["properties"])["targetType"])["pattern"])
	assert.NotContains(t, string(WatcherSchema()), `"enum"`)
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
