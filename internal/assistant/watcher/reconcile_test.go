//nolint:testpackage // Test normalized comparison and source-read ordering directly.
package watcher

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/assistant/watchers/watcherstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func minimalWatcher(t *testing.T) Watcher {
	t.Helper()
	var w Watcher
	require.NoError(t, json.Unmarshal([]byte(`{"title":"Example","prompt":"Watch errors"}`), &w))
	return w
}
func TestOpenValuesRetainStrictShape(t *testing.T) {
	for _, field := range []string{"sensitivity", "slack", "teams", "webhook"} {
		t.Run(field, func(t *testing.T) {
			w := minimalWatcher(t)
			path := "spec.sensitivity"
			if field == "sensitivity" {
				w.Sensitivity = "future-value"
			} else {
				path = "spec.notifications." + field + ".severity"
				switch field {
				case "slack":
					w.Notifications.Slack.Severity = "future-value"
				case "teams":
					w.Notifications.Teams.Severity = "future-value"
				case "webhook":
					w.Notifications.Webhook.Severity = "future-value"
				}
			}
			data, err := json.Marshal(w)
			require.NoError(t, err)
			var roundtrip Watcher
			require.NoError(t, json.Unmarshal(data, &roundtrip))
			require.NoError(t, roundtrip.Validate())
			warnings := roundtrip.Warnings()
			require.Len(t, warnings, 1)
			assert.Contains(t, warnings[0], path)
			assert.Contains(t, warnings[0], "future-value")
			assert.Contains(t, warnings[0], "known values:")
		})
	}
	var schema map[string]any
	require.NoError(t, json.Unmarshal(WatcherSchema(), &schema))
	assert.NotContains(t, string(WatcherSchema()), `"enum"`)
}
func TestDecodeManifestRetainsID(t *testing.T) {
	w := minimalWatcher(t)
	raw, err := json.Marshal(w)
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, json.Unmarshal(raw, &spec))
	obj := &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	obj.SetName("example")
	obj.SetAPIVersion(WatcherAPIVersion)
	obj.SetKind(WatcherKind)
	obj.SetAnnotations(map[string]string{WatcherIDAnnotation: "id-1"})
	decoded, err := DecodeManifest(obj)
	require.NoError(t, err)
	assert.Equal(t, "id-1", decoded.ServerID())
	bytes, err := json.Marshal(decoded)
	require.NoError(t, err)
	assert.NotContains(t, string(bytes), "id-1")
}
func TestNormalizedEffectiveConfiguration(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Watcher, *Watcher)
	}{
		{"equivalent duration", func(a, b *Watcher) { a.Interval = "600s"; b.Interval = "10m" }},
		{"effective cadence", func(a, b *Watcher) { a.Interval = "90m"; b.Interval = "2h" }},
		{"collection order", func(a, b *Watcher) {
			a.DatasourceUIDs = []string{"b", "a"}
			b.DatasourceUIDs = []string{"a", "b"}
			a.Investigation.TeamAccess = []string{"two", "one"}
			b.Investigation.TeamAccess = []string{"one", "two"}
		}},
		{"timestamp", func(a, b *Watcher) {
			a.AutoStop = AutoStop{Enabled: true, At: "2030-01-01T01:00:00+01:00"}
			b.AutoStop = AutoStop{Enabled: true, At: "2030-01-01T00:00:00Z"}
		}},
		{"observations secrets enrollment", func(a, b *Watcher) {
			a.serverID = "id"
			a.AutomaticRecalibration.Enabled = true
			a.Notifications.Webhook.URL = &SecretInput{Preserve: true}
			b.Labels = nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := minimalWatcher(t), minimalWatcher(t)
			tt.edit(&a, &b)
			assert.Equal(t, normalized(a), normalized(b))
		})
	}
}
func TestSecretIntentBeforeResolution(t *testing.T) {
	tests := []struct {
		name             string
		create           bool
		desired, current WebhookNotification
		want             string
	}{
		{"preserve create", true, WebhookNotification{URL: &SecretInput{Preserve: true}}, WebhookNotification{}, "cannot preserve"},
		{"URL supplied with stored credential", false, WebhookNotification{URL: &SecretInput{FromEnv: "DO_NOT_READ"}}, WebhookNotification{BearerToken: &SecretInput{Preserve: true}}, "every saved"},
		{"URL cleared with preserved signing secret", false, WebhookNotification{URL: &SecretInput{Clear: true}, SigningSecret: &SecretInput{Preserve: true}}, WebhookNotification{SigningSecret: &SecretInput{Preserve: true}}, "every saved"},
		{"all cleared", false, WebhookNotification{URL: &SecretInput{Clear: true}, BearerToken: &SecretInput{Clear: true}}, WebhookNotification{BearerToken: &SecretInput{Preserve: true}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := minimalWatcher(t), minimalWatcher(t)
			a.Notifications.Webhook = tt.desired
			b.Notifications.Webhook = tt.current
			err := checkSecretIntent(a, b, tt.create)
			if tt.want != "" {
				require.ErrorContains(t, err, tt.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
	t.Run("file trims trailing line endings", func(t *testing.T) {
		file := t.TempDir() + "/secret"
		writeSecretTestFile(t, file)
		value, err := resolveSecret(&SecretInput{FromFile: file})
		require.NoError(t, err)
		assert.Equal(t, "test-secret", *value)
	})
}
func TestWriteMappingCompleteness(t *testing.T) {
	w := minimalWatcher(t)
	request := configurationRequest(w)
	data, err := json.Marshal(request)
	require.NoError(t, err)
	var object map[string]any
	require.NoError(t, json.Unmarshal(data, &object))
	for _, key := range []string{"name", "description", "prompt", "datasourceUids", "triggerIntervalSeconds", "sensitivity", "disableDecisionSkip", "autoStop", "labels", "actions"} {
		assert.Contains(t, object, key)
	}
	for _, key := range []string{"queries", "calibrationContext", "staticLogPolicies", "definitionVersion"} {
		assert.NotContains(t, object, key)
	}
	assert.Empty(t, *request.DatasourceUIDs)
	assert.NotNil(t, request.Actions.Slack)
	assert.NotNil(t, request.Actions.MSTeams)
	assert.NotNil(t, request.Actions.Alerting)
	assert.NotNil(t, request.Actions.Webhook)
	assert.NotNil(t, request.Actions.Investigation)
	assert.False(t, *request.Actions.Webhook.Enabled)
}
func TestWriteIdentity(t *testing.T) {
	index := map[string][]Candidate{"same-name": {{ID: "one", Title: "Same/name", Name: "same-name"}, {ID: "two", Title: "Same name", Name: "same-name"}}, "archived": {{ID: "three", Title: "Archived", Name: "archived"}}}
	tests := []struct {
		name, id, title string
		create          bool
		wantID, err     string
	}{
		{"exact title", "", "Same name", false, "two", ""}, {"stale ID fallback", "stale", "Same name", false, "two", ""}, {"ID rename", "one", "New title", false, "one", ""}, {"occupied rename", "one", "Archived", false, "", "occupied"}, {"true create existing", "", "Same name", true, "", "already exists"}, {"new title", "", "New", false, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := minimalWatcher(t)
			w.Title = tt.title
			w.serverID = tt.id
			target, err := selectUpsert(index, w, tt.create)
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantID, target.ID)
			}
		})
	}
	_, err := resolveIdentity(index, "same-name")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "one") && strings.Contains(err.Error(), "two"))
	raw := watchers.Watcher{ID: "id", Name: "Example", TriggerIntervalSeconds: 600, Sensitivity: "future"}
	assert.Equal(t, "future", WatcherFromResponse(raw, false).Sensitivity)
}

func writeSecretTestFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("test-secret\r\n"), 0600))
}

func TestReconcileIndependentWrites(t *testing.T) {
	tests := []struct {
		name                                  string
		edit                                  func(*Watcher)
		enrollmentFail                        bool
		configurationWrites, enrollmentWrites int
		partial                               bool
	}{
		{"unchanged", nil, false, 0, 0, false},
		{"definition replacement", func(w *Watcher) { w.Description = "Updated" }, false, 1, 0, false},
		{"enrollment only", func(w *Watcher) { w.AutomaticRecalibration.Enabled = true }, false, 0, 1, false},
		{"definition saved enrollment failed", func(w *Watcher) { w.Description = "Updated"; w.AutomaticRecalibration.Enabled = true }, true, 1, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := watchers.Watcher{ID: "id-one", Name: "Example", Prompt: "Watch errors", TriggerIntervalSeconds: 600, Sensitivity: "balanced", DefinitionVersion: 1}
			fixture := &watcherstest.Server{AllowWrites: true, Current: [][]watchers.Watcher{{raw}}, Enrollment: map[string]bool{"id-one": false}}
			if tt.enrollmentFail {
				fixture.EnrollmentWriteStatus = map[string]int{"id-one": http.StatusServiceUnavailable}
			}
			client := clientForServer(t, fixture.ServeHTTP)
			desired := WatcherFromResponse(raw, false)
			if tt.edit != nil {
				tt.edit(&desired)
			}
			result, err := reconcile(t.Context(), client, desired, raw.ID)
			if tt.partial {
				var partial *PartialCompletionError
				require.ErrorAs(t, err, &partial)
				assert.Equal(t, raw.ID, partial.Watcher.ServerID())
				assert.Equal(t, "saved", partial.Watcher.MutationOutcome().Configuration)
				assert.Equal(t, "failed", partial.Watcher.MutationOutcome().Enrollment)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.configurationWrites+tt.enrollmentWrites > 0, result.MutationOutcome().Changed)
			}
			var config, enrollment int
			for _, request := range fixture.MutationRequests() {
				if strings.HasSuffix(request.Path, "/auto-calibration") {
					enrollment++
				} else {
					config++
				}
			}
			assert.Equal(t, tt.configurationWrites, config)
			assert.Equal(t, tt.enrollmentWrites, enrollment)
			if tt.partial {
				fixture.EnrollmentWriteStatus = nil
				result, err = reconcile(t.Context(), client, desired, raw.ID)
				require.NoError(t, err)
				assert.Equal(t, "unchanged", result.MutationOutcome().Configuration)
				assert.Equal(t, "saved", result.MutationOutcome().Enrollment)
			}
		})
	}
}
func TestReconcileRefusesBeforeSources(t *testing.T) {
	tests := []struct {
		name       string
		detail     string
		enrollment watchers.Enrollment
	}{
		{"targeting", `{"slack":{"enabled":false,"target":{"type":"dm","userId":"private"}}}`, watchers.Enrollment{}},
		{"alert routing", `{"alerting":{"routingTree":"custom"}}`, watchers.Enrollment{}},
		{"unknown webhook setting", `{"webhook":{"futureOption":"private"}}`, watchers.Enrollment{}},
		{"custom schedule", `{}`, watchers.Enrollment{ScheduleCron: "0 1 * * *"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := watchers.Watcher{ID: "id", Name: "Example", Prompt: "Watch errors", TriggerIntervalSeconds: 600, Sensitivity: "balanced"}
			require.NoError(t, json.Unmarshal([]byte(tt.detail), &raw.Actions))
			fixture := &watcherstest.Server{AllowWrites: true, Current: [][]watchers.Watcher{{raw}}, EnrollmentDetails: map[string]watchers.Enrollment{"id": tt.enrollment}}
			detail, _ := json.Marshal(raw)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(detail, &fields))
			fields["actions"] = json.RawMessage(tt.detail)
			detail, err := json.Marshal(fields)
			require.NoError(t, err)
			fixture.RawDetails = map[string]json.RawMessage{"id": detail}
			client := clientForServer(t, fixture.ServeHTTP)
			w := WatcherFromResponse(raw, false)
			w.Notifications.Webhook.URL = &SecretInput{FromFile: "/must-not-read"}
			_, err = reconcile(t.Context(), client, w, raw.ID)
			require.ErrorContains(t, err, "unrepresented")
			assert.Zero(t, fixture.MutationCalls())
			assert.NotContains(t, err.Error(), "private")
		})
	}
}
