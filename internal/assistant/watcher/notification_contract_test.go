//nolint:testpackage // Verify shared reconciliation payloads and no-op behavior.
package watcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNotificationGenericPushAndTypedRead(t *testing.T) {
	raw := contractRaw()
	fixture := contractFixture(raw)
	a, _ := acceptanceAdapter(t, fixture)
	resource := acceptanceManifest(t, raw.Name, raw.ID)
	require.NoError(t, unstructured.SetNestedField(resource.Object.Object, map[string]any{
		"slack":    map[string]any{"targetType": "dm", "userId": "U_EXAMPLE", "teamId": "T_EXAMPLE"},
		"alerting": map[string]any{"severity": "critical", "routingTree": "operations"},
	}, "spec", "notifications"))
	input := resources.NewResources()
	input.AddDistinct(resource)
	summary := acceptancePush(t, a, input, false)
	require.Zero(t, summary.FailedCount())
	require.Equal(t, 1, summary.SuccessCount())
	assert.Equal(t, 1, fixture.MutationCalls())
	client := clientForServer(t, fixture.ServeHTTP)
	read, err := NewTypedCRUDForClient(client, "example").Get(t.Context(), raw.ID)
	require.NoError(t, err)
	assert.Equal(t, "dm", read.Spec.Notifications.Slack.TargetType)
	assert.Equal(t, "U_EXAMPLE", read.Spec.Notifications.Slack.UserID)
	assert.Equal(t, "operations", read.Spec.Notifications.Alerting.RoutingTree)
	assert.Equal(t, "critical", read.Spec.Notifications.Alerting.Severity)
	require.Zero(t, acceptancePush(t, a, input, false).FailedCount())
	assert.Equal(t, 1, fixture.MutationCalls())
}

func TestNotificationConfigurationRoundTrip(t *testing.T) {
	for _, tt := range []struct {
		name            string
		slack           *watchers.ChatTarget
		severity, route string
	}{
		{name: "channel workspace", slack: &watchers.ChatTarget{Type: "channel", ChannelID: "C_EXAMPLE", TeamID: "T_EXAMPLE"}, severity: "warning", route: "operations"},
		{name: "direct message", slack: &watchers.ChatTarget{Type: "dm", UserID: "U_EXAMPLE", TeamID: "T_EXAMPLE"}, severity: "critical", route: "operations"},
		{name: "unused target identifiers retained", slack: &watchers.ChatTarget{Type: "dm", UserID: "U_EXAMPLE", ChannelID: "C_EXAMPLE", TeamID: "T_EXAMPLE"}, severity: "critical", route: "operations"},
		{name: "future vocabulary", slack: &watchers.ChatTarget{Type: "future-target", UserID: "U_EXAMPLE"}, severity: "future-severity", route: "future-tree"},
		{name: "legacy default route", severity: "warning", route: "default"},
		{name: "legacy user-defined route", severity: "warning", route: "user-defined"},
		{name: "legacy route whitespace", severity: "warning", route: " operations "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := contractRaw()
			raw.Actions.Slack = &watchers.ChatAction{Enabled: false, Target: tt.slack, MinSeverity: "critical"}
			raw.Actions.Alerting = &watchers.AlertingAction{Enabled: false, MinSeverity: tt.severity, RoutingTree: new(tt.route)}
			fixture := contractFixture(raw)
			client := clientForServer(t, fixture.ServeHTTP)
			desired, err := ReadManifest(t.Context(), client, raw)
			require.NoError(t, err)
			data, err := json.Marshal(desired)
			require.NoError(t, err)
			var authored Watcher
			require.NoError(t, json.Unmarshal(data, &authored))
			result, err := reconcile(t.Context(), client, authored, raw.ID)
			require.NoError(t, err)
			assert.False(t, result.MutationOutcome().Changed)
			assert.Zero(t, fixture.MutationCalls())
			assert.Equal(t, tt.route, result.Notifications.Alerting.RoutingTree)
			if tt.slack != nil {
				assert.Equal(t, tt.slack.Type, result.Notifications.Slack.TargetType)
				assert.Equal(t, tt.slack.ChannelID, result.Notifications.Slack.ChannelID)
				assert.Equal(t, tt.slack.UserID, result.Notifications.Slack.UserID)
				assert.Equal(t, tt.slack.TeamID, result.Notifications.Slack.TeamID)
			}
			// A separate configuration edit retransmits every modeled destination.
			authored.Description = "Edited"
			result, err = reconcile(t.Context(), client, authored, raw.ID)
			require.NoError(t, err)
			require.Equal(t, 1, fixture.MutationCalls())
			request := contractRequest(t, fixture, 0)
			actions := testObject(t, request["actions"])
			alerting := testObject(t, actions["alerting"])
			assert.Equal(t, tt.route, alerting["routingTree"])
			assert.Equal(t, tt.severity, alerting["minSeverity"])
			if tt.slack != nil {
				target := testObject(t, testObject(t, actions["slack"])["target"])
				expected, err := json.Marshal(tt.slack)
				require.NoError(t, err)
				actual, err := json.Marshal(target)
				require.NoError(t, err)
				assert.JSONEq(t, string(expected), string(actual))
			}
			assert.Equal(t, desired.Notifications, result.Notifications)
			_, err = reconcile(t.Context(), client, authored, raw.ID)
			require.NoError(t, err)
			assert.Equal(t, 1, fixture.MutationCalls())
			contractNoLifecycle(t, fixture)
		})
	}
}

func TestAlertingRoutingSelection(t *testing.T) {
	for _, tt := range []struct {
		name                  string
		current               *string
		legacy, desired, want string
		writes                int
	}{
		{name: "legacy label projects route", legacy: "operations", desired: "operations", want: "operations"},
		{name: "explicit route overrides label", current: new("explicit"), legacy: "legacy", desired: "explicit", want: "explicit"},
		{name: "explicit default overrides label", current: new(""), legacy: "legacy", want: ""},
		{name: "new route overrides label", legacy: "legacy", desired: "new-route", want: "new-route", writes: 1},
		{name: "reset named route", current: new("operations"), want: "", writes: 1},
		{name: "reset legacy alias", current: new("default"), want: "", writes: 1},
		{name: "default alias resets", current: new("operations"), desired: " default ", want: "", writes: 1},
		{name: "user-defined alias resets", current: new("operations"), desired: "user-defined", want: "", writes: 1},
		{name: "new name trims", current: new("previous"), desired: " operations ", want: "operations", writes: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := contractRaw()
			raw.Labels = map[string]string{"__grafana_managed_route__": tt.legacy, "service": "example"}
			raw.Actions.Alerting = &watchers.AlertingAction{MinSeverity: "warning", RoutingTree: tt.current}
			fixture := contractFixture(raw)
			client := clientForServer(t, fixture.ServeHTTP)
			desired := WatcherFromResponse(raw, false)
			desired.Notifications.Alerting.RoutingTree = tt.desired
			// Previously exported manifests may still carry the legacy label.
			desired.Labels["__grafana_managed_route__"] = tt.legacy
			originalLabels := map[string]string{"__grafana_managed_route__": tt.legacy, "service": "example"}
			result, err := reconcile(t.Context(), client, desired, raw.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result.Notifications.Alerting.RoutingTree)
			assert.Equal(t, tt.writes, fixture.MutationCalls())
			assert.Equal(t, originalLabels, desired.Labels)
			if tt.writes > 0 {
				actions := testObject(t, contractRequest(t, fixture, 0)["actions"])
				assert.Equal(t, tt.want, testObject(t, actions["alerting"])["routingTree"])
			}
			_, err = reconcile(t.Context(), client, desired, raw.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.writes, fixture.MutationCalls())
		})
	}
}

func TestNotificationAuthoritativeReplacement(t *testing.T) {
	raw := contractRaw()
	raw.Actions.Slack = &watchers.ChatAction{Target: &watchers.ChatTarget{Type: "dm", UserID: "U_EXAMPLE", TeamID: "T_EXAMPLE"}, MinSeverity: "critical"}
	raw.Actions.Alerting = &watchers.AlertingAction{MinSeverity: "critical", RoutingTree: new("operations")}
	fixture := contractFixture(raw)
	client := clientForServer(t, fixture.ServeHTTP)
	result, err := reconcile(t.Context(), client, minimalWatcher(t), raw.ID)
	require.NoError(t, err)
	assert.Equal(t, minimalWatcher(t).Notifications, result.Notifications)
	require.Equal(t, 1, fixture.MutationCalls())
	actions := testObject(t, contractRequest(t, fixture, 0)["actions"])
	assert.Nil(t, testObject(t, actions["slack"])["target"])
	assert.Equal(t, "warning", testObject(t, actions["alerting"])["minSeverity"])
	assert.Empty(t, testObject(t, actions["alerting"])["routingTree"])
	_, err = reconcile(t.Context(), client, minimalWatcher(t), raw.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, fixture.MutationCalls())
}

func TestNotificationSeverityWireAliasNoop(t *testing.T) {
	raw := contractRaw()
	raw.Actions = watchers.Actions{
		Slack:    &watchers.ChatAction{MinSeverity: "warning"},
		MSTeams:  &watchers.ChatAction{MinSeverity: "warning"},
		Alerting: &watchers.AlertingAction{MinSeverity: "warning"},
		Webhook:  &watchers.WebhookAction{MinSeverity: "warning"},
	}
	fixture := contractFixture(raw)
	client := clientForServer(t, fixture.ServeHTTP)
	desired := WatcherFromResponse(raw, false)
	desired.Notifications.Slack.Severity = "warning"
	desired.Notifications.Teams.Severity = "warning"
	desired.Notifications.Alerting.Severity = "warning"
	desired.Notifications.Webhook.Severity = "warning"
	_, err := reconcile(t.Context(), client, desired, raw.ID)
	require.NoError(t, err)
	assert.Zero(t, fixture.MutationCalls())
}

func TestNotificationLegacyRouteWithoutAlertingBlock(t *testing.T) {
	raw := contractRaw()
	raw.Labels = map[string]string{"__grafana_managed_route__": "legacy-route"}
	fixture := contractFixture(raw)
	client := clientForServer(t, fixture.ServeHTTP)
	desired, err := ReadManifest(t.Context(), client, raw)
	require.NoError(t, err)
	assert.Equal(t, "legacy-route", desired.Notifications.Alerting.RoutingTree)
	_, err = reconcile(t.Context(), client, desired, raw.ID)
	require.NoError(t, err)
	assert.Zero(t, fixture.MutationCalls())
	desired.Description = "Edited"
	_, err = reconcile(t.Context(), client, desired, raw.ID)
	require.NoError(t, err)
	actions := testObject(t, contractRequest(t, fixture, 0)["actions"])
	assert.Equal(t, "legacy-route", testObject(t, actions["alerting"])["routingTree"])
}

func TestDisabledLegacySlackTargetsRemainReadable(t *testing.T) {
	for _, target := range []watchers.ChatTarget{
		{Type: "channel", UserID: "U_EXAMPLE"},
		{Type: "channel", TeamID: "T_EXAMPLE"},
		{Type: "channel", UserID: "U_EXAMPLE", TeamID: "T_EXAMPLE"},
	} {
		t.Run(target.UserID+target.TeamID, func(t *testing.T) {
			raw := contractRaw()
			raw.Actions.Slack = &watchers.ChatAction{Target: &target, MinSeverity: "warning"}
			fixture := contractFixture(raw)
			client := clientForServer(t, fixture.ServeHTTP)
			crud := NewTypedCRUDForClient(client, "example")
			got, err := crud.Get(t.Context(), raw.ID)
			require.NoError(t, err)
			assert.Equal(t, target.UserID, got.Spec.Notifications.Slack.UserID)
			assert.Equal(t, target.TeamID, got.Spec.Notifications.Slack.TeamID)
			items, err := crud.List(t.Context(), 0)
			require.NoError(t, err)
			require.Len(t, items, 1)
			assert.Equal(t, got.Spec, items[0].Spec)
			data, err := json.Marshal(got.Spec)
			require.NoError(t, err)
			var decoded Watcher
			require.NoError(t, json.Unmarshal(data, &decoded))
			result, err := reconcile(t.Context(), client, decoded, raw.ID)
			require.NoError(t, err)
			assert.False(t, result.MutationOutcome().Changed)
			assert.Zero(t, fixture.MutationCalls())
		})
	}
}

func TestAlertingLegacyResetWithOmittedRouteResponse(t *testing.T) {
	raw := contractRaw()
	raw.Labels = map[string]string{"__grafana_managed_route__": "operations", "service": "example"}
	raw.Actions.Alerting = &watchers.AlertingAction{MinSeverity: "warning"}
	fixture := contractFixture(raw)
	client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		fixture.ServeHTTP(recorder, r)
		var envelope map[string]any
		if !assert.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope)) {
			http.Error(w, "invalid fixture response", http.StatusInternalServerError)
			return
		}
		if data, ok := envelope["data"].(map[string]any); ok {
			if actions, ok := data["actions"].(map[string]any); ok {
				if alerting, ok := actions["alerting"].(map[string]any); ok {
					delete(alerting, "routingTree")
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(recorder.Code)
		assert.NoError(t, json.NewEncoder(w).Encode(envelope))
	})
	desired, err := ReadManifest(t.Context(), client, raw)
	require.NoError(t, err)
	assert.Equal(t, "operations", desired.Notifications.Alerting.RoutingTree)
	assert.Equal(t, map[string]string{"service": "example"}, desired.Labels)
	// Keep a stale label to exercise manifests exported before the dedicated field.
	desired.Labels["__grafana_managed_route__"] = "operations"
	desired.Notifications.Alerting.RoutingTree = ""
	result, err := reconcile(t.Context(), client, desired, raw.ID)
	require.NoError(t, err)
	assert.Empty(t, result.Notifications.Alerting.RoutingTree)
	assert.Equal(t, map[string]string{"service": "example"}, result.Labels)
	assert.Equal(t, "operations", desired.Labels["__grafana_managed_route__"])
	require.Equal(t, 1, fixture.MutationCalls())
	request := contractRequest(t, fixture, 0)
	assert.Equal(t, map[string]any{"service": "example"}, request["labels"])
	actions := testObject(t, request["actions"])
	assert.Empty(t, testObject(t, actions["alerting"])["routingTree"])
	_, err = reconcile(t.Context(), client, desired, raw.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, fixture.MutationCalls())
	contractNoLifecycle(t, fixture)
}
