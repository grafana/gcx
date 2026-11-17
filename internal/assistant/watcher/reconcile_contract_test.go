//nolint:testpackage // Acceptance tests exercise owned reconciliation without transport duplication.
package watcher

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/assistant/watchers/watcherstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func contractRaw() watchers.Watcher {
	return watchers.Watcher{ID: "contract-id", Name: "Example", Prompt: "Watch errors", TriggerIntervalSeconds: 600, Sensitivity: "balanced", DefinitionVersion: 7}
}
func contractFixture(raw watchers.Watcher) *watcherstest.Server {
	return &watcherstest.Server{AllowWrites: true, Current: [][]watchers.Watcher{{raw}}, Enrollment: map[string]bool{raw.ID: false}}
}
func contractRequest(t *testing.T, fixture *watcherstest.Server, index int) map[string]any {
	t.Helper()
	requests := fixture.MutationRequests()
	require.Greater(t, len(requests), index)
	var request map[string]any
	require.NoError(t, json.Unmarshal(requests[index].Body, &request))
	return request
}
func contractCompletePayload(t *testing.T, request map[string]any) {
	t.Helper()
	assert.Len(t, request, 10)
	for _, key := range []string{"name", "description", "prompt", "triggerIntervalSeconds", "sensitivity", "disableDecisionSkip", "datasourceUids", "labels", "autoStop", "actions"} {
		assert.Contains(t, request, key)
	}
	actions := testObject(t, request["actions"])
	assert.Len(t, actions, 5)
	for _, key := range []string{"slack", "msteams", "webhook", "alerting", "investigation"} {
		assert.Contains(t, actions, key)
	}
}
func contractNoLifecycle(t *testing.T, fixture *watcherstest.Server) {
	t.Helper()
	for _, request := range fixture.MutationRequests() {
		assert.Contains(t, []string{http.MethodPost, http.MethodPut}, request.Method)
		assert.True(t, request.Path == testCollectionPath || request.Path == testCollectionPath+"/contract-id" || strings.HasSuffix(request.Path, "/auto-calibration"), request.Path)
	}
}

func TestContractReplacementClearsModeledSettingsPreservesCalibration(t *testing.T) {
	raw := contractRaw()
	raw.Description = "Remove this"
	raw.DatasourceUIDs = []string{"old-source"}
	raw.Labels = map[string]string{"team": "old-team"}
	deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	raw.AutoStop = &watchers.AutoStop{PauseAt: &deadline, Archive: true}
	raw.Actions = watchers.Actions{
		Slack:         &watchers.ChatAction{Enabled: true, Target: &watchers.ChatTarget{Type: "channel", ChannelID: "slack-channel"}, MinSeverity: "critical"},
		MSTeams:       &watchers.ChatAction{Enabled: true, Target: &watchers.ChatTarget{Type: "channel", ChannelID: "teams-channel"}, MinSeverity: "critical"},
		Alerting:      &watchers.AlertingAction{Enabled: true},
		Investigation: &watchers.InvestigationAction{Enabled: true, TeamNames: []string{"team"}},
		Webhook:       &watchers.WebhookAction{Enabled: true, MinSeverity: "critical", SecureFields: &watchers.SecureFields{URL: true, HMACSecret: true, AuthorizationCredentials: true}},
	}
	raw.Status = "running"
	raw.Queries = []map[string]any{{"id": "query", "type": "logql", "expr": "calibrated expression", "futureParameter": "retained"}}
	raw.CalibrationContext = "calibrated guidance"
	raw.StaticLogPolicies = map[string]any{"query": map[string]any{"patterns": []any{"calibrated policy"}}}
	fixture := contractFixture(raw)
	client := clientForServer(t, fixture.ServeHTTP)
	desired := minimalWatcher(t)
	result, err := reconcile(t.Context(), client, desired, raw.ID)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, fixture.MutationCalls())
	assert.JSONEq(t, `{"name":"Example","description":"","prompt":"Watch errors","triggerIntervalSeconds":600,"sensitivity":"balanced","disableDecisionSkip":false,"datasourceUids":[],"labels":{},"autoStop":{},"actions":{"slack":{"enabled":false,"target":null,"minSeverity":"warning"},"msteams":{"enabled":false,"target":null,"minSeverity":"warning"},"webhook":{"enabled":false,"minSeverity":"warning"},"alerting":{"enabled":false,"minSeverity":"warning","routingTree":""},"investigation":{"enabled":false,"teamNames":[]}}}`, string(fixture.MutationRequests()[0].Body))
	stored, err := client.Get(t.Context(), raw.ID)
	require.NoError(t, err)
	assert.Empty(t, stored.Description)
	assert.Empty(t, stored.DatasourceUIDs)
	assert.Empty(t, stored.Labels)
	require.NotNil(t, stored.AutoStop)
	assert.Nil(t, stored.AutoStop.PauseAt)
	assert.False(t, stored.AutoStop.Archive)
	assert.Equal(t, raw.Queries, stored.Queries)
	assert.Equal(t, raw.StaticLogPolicies, stored.StaticLogPolicies)
	assert.Equal(t, raw.CalibrationContext, stored.CalibrationContext)
	assert.Equal(t, raw.DefinitionVersion, stored.DefinitionVersion)
	assert.Equal(t, "running", stored.Status)
	assert.False(t, result.Notifications.Webhook.Enabled)
	require.NotNil(t, result.Notifications.Webhook.URL)
	assert.True(t, result.Notifications.Webhook.URL.Preserve)
	assert.True(t, result.Notifications.Webhook.BearerToken.Preserve)
	assert.True(t, result.Notifications.Webhook.SigningSecret.Preserve)
	contractNoLifecycle(t, fixture)
}

func TestContractEquivalentConfigurationWritesNothing(t *testing.T) {
	tests := []struct {
		name    string
		raw     func(*watchers.Watcher)
		desired func(*Watcher)
	}{
		{"pulled defaults", nil, nil},
		{"equivalent duration", nil, func(w *Watcher) { w.Interval = "600s" }},
		{"effective cadence", func(r *watchers.Watcher) { r.TriggerIntervalSeconds = 7200 }, func(w *Watcher) { w.Interval = "90m" }},
		{"timestamp instant", func(r *watchers.Watcher) {
			deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			r.AutoStop = &watchers.AutoStop{PauseAt: &deadline}
		}, func(w *Watcher) { w.AutoStop.At = "2030-01-01T01:00:00+01:00" }},
		{"unordered collections", func(r *watchers.Watcher) {
			r.DatasourceUIDs = []string{"b", "a"}
			r.Actions.Investigation = &watchers.InvestigationAction{Enabled: true, TeamNames: []string{"two", "one"}}
		}, func(w *Watcher) {
			w.DatasourceUIDs = []string{"a", "b", "a"}
			w.Investigation.TeamAccess = []string{"one", "two"}
		}},
		{"legacy label and stop aliases", func(r *watchers.Watcher) {
			deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			r.Temporary = &watchers.Temporary{EndsAt: &deadline}
			r.Actions.Alerting = &watchers.AlertingAction{Labels: map[string]string{"team": "example"}}
		}, nil},
		{"default destination objects", func(r *watchers.Watcher) {
			r.Actions.Slack = &watchers.ChatAction{MinSeverity: "warning", Target: &watchers.ChatTarget{Type: "channel"}}
			r.Actions.Alerting = &watchers.AlertingAction{MinSeverity: "warning", RoutingTree: new("")}
			r.Actions.Webhook = &watchers.WebhookAction{MinSeverity: "warning", AuthorizationScheme: "Bearer"}
		}, nil},
		{"disabled webhook secrets omitted", func(r *watchers.Watcher) {
			r.Actions.Webhook = &watchers.WebhookAction{SecureFields: &watchers.SecureFields{URL: true, HMACSecret: true, AuthorizationCredentials: true}}
		}, func(w *Watcher) {
			w.Notifications.Webhook.URL = nil
			w.Notifications.Webhook.BearerToken = nil
			w.Notifications.Webhook.SigningSecret = nil
		}},
		{"absent URL clear preserves stored credentials", func(r *watchers.Watcher) {
			r.Actions.Webhook = &watchers.WebhookAction{SecureFields: &watchers.SecureFields{AuthorizationCredentials: true}}
		}, func(w *Watcher) { w.Notifications.Webhook.URL = &SecretInput{Clear: true} }},
		{"absent secret clearing", nil, func(w *Watcher) {
			w.Notifications.Webhook.URL = &SecretInput{Clear: true}
			w.Notifications.Webhook.BearerToken = &SecretInput{Clear: true}
			w.Notifications.Webhook.SigningSecret = &SecretInput{Clear: true}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := contractRaw()
			if tt.raw != nil {
				tt.raw(&raw)
			}
			fixture := contractFixture(raw)
			fixture.EnrollmentDetails = map[string]watchers.Enrollment{raw.ID: {Enabled: false, ScheduleCron: "0 9 * * 1", ScheduleTimezone: "UTC"}}
			client := clientForServer(t, fixture.ServeHTTP)
			observed, err := client.Get(t.Context(), raw.ID)
			require.NoError(t, err)
			desired := WatcherFromResponse(*observed, false)
			if tt.desired != nil {
				tt.desired(&desired)
			}
			result, err := reconcile(t.Context(), client, desired, raw.ID)
			require.NoError(t, err)
			assert.False(t, result.MutationOutcome().Changed)
			assert.Equal(t, "unchanged", result.MutationOutcome().Configuration)
			assert.Zero(t, fixture.MutationCalls())
		})
	}
}

func TestContractIntervalPayloadMatchesEffectiveComparison(t *testing.T) {
	for _, tc := range []struct {
		interval string
		seconds  int
	}{
		{"6m", 360},
		{"7m", 360},
		{"8m", 360},
		{"600s", 600},
		{"90m", 7200},
	} {
		t.Run(tc.interval, func(t *testing.T) {
			fixture := &watcherstest.Server{AllowWrites: true}
			client := clientForServer(t, fixture.ServeHTTP)
			desired := minimalWatcher(t)
			desired.Interval = tc.interval
			created, err := createConfiguration(t.Context(), client, &desired)
			require.NoError(t, err)
			require.Equal(t, 1, fixture.MutationCalls())
			assert.InDelta(t, tc.seconds, contractRequest(t, fixture, 0)["triggerIntervalSeconds"], 0)
			assert.Equal(t, normalized(desired).Interval, normalized(*created).Interval)

			unchanged, err := updateConfiguration(t.Context(), client, created.ServerID(), &desired)
			require.NoError(t, err)
			assert.False(t, unchanged.MutationOutcome().Changed)
			assert.Equal(t, 1, fixture.MutationCalls())

			desired.Interval = "15m"
			updated, err := updateConfiguration(t.Context(), client, created.ServerID(), &desired)
			require.NoError(t, err)
			assert.True(t, updated.MutationOutcome().Changed)
			require.Equal(t, 2, fixture.MutationCalls())
			assert.InDelta(t, 900, contractRequest(t, fixture, 1)["triggerIntervalSeconds"], 0)
		})
	}
}

func TestContractAPIControlsEditEligibility(t *testing.T) {
	for _, tt := range []struct {
		name         string
		archived     bool
		status       string
		response     int
		body, reason string
	}{
		{"archived rejected", true, "paused", 409, `{"error":"restore the archived watcher first SECRET-ECHO"}`, "restore the archived Watcher"},
		{"calibrating rejected", false, "calibrating", 409, `{"message":"finish or cancel calibration before updating the watcher SECRET-ECHO"}`, "finish or cancel calibration"},
		{"archived accepted", true, "paused", 0, "", ""},
		{"calibrating accepted", false, "calibrating", 0, "", ""},
		{"running accepted", false, "running", 0, "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := contractRaw()
			raw.Status = tt.status
			if tt.archived {
				raw.ArchivedAt = new(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			}
			fixture := contractFixture(raw)
			fixture.UpdateStatus = map[string]int{raw.ID: tt.response}
			fixture.ErrorBody = tt.body
			desired := WatcherFromResponse(raw, false)
			desired.Description = "Edit"
			desired.AutomaticRecalibration.Enabled = true
			result, err := reconcile(t.Context(), clientForServer(t, fixture.ServeHTTP), desired, raw.ID)
			if tt.response != 0 {
				require.ErrorIs(t, err, watchers.ErrConflict)
				require.ErrorContains(t, err, tt.reason)
				assert.NotContains(t, err.Error(), "SECRET-ECHO")
				assert.Nil(t, result)
				assert.Equal(t, 1, fixture.MutationCalls())
			} else {
				require.NoError(t, err)
				assert.Equal(t, "saved", result.MutationOutcome().Configuration)
				assert.Equal(t, 2, fixture.MutationCalls())
			}
			contractNoLifecycle(t, fixture)
		})
	}
}

func TestContractModeledReadsFailBeforeMutation(t *testing.T) {
	for _, tt := range []struct {
		name               string
		detail, enrollment int
		kind               error
	}{
		{"detail denied", 403, 0, watchers.ErrPermissionDenied},
		{"enrollment unavailable", 0, 501, watchers.ErrCapabilityUnavailable},
		{"enrollment denied", 0, 403, watchers.ErrPermissionDenied},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := contractRaw()
			fixture := contractFixture(raw)
			fixture.DetailStatus = map[string]int{raw.ID: tt.detail}
			fixture.EnrollmentStatus = map[string]int{raw.ID: tt.enrollment}
			desired := minimalWatcher(t)
			desired.Description = "Edit"
			desired.Notifications.Webhook.SigningSecret = &SecretInput{FromFile: filepath.Join(t.TempDir(), "unreadable")}
			_, err := reconcile(t.Context(), clientForServer(t, fixture.ServeHTTP), desired, raw.ID)
			require.ErrorIs(t, err, tt.kind)
			assert.Zero(t, fixture.MutationCalls())
		})
	}
}

func TestContractDeferredSettingsRefuseBeforeSourceResolution(t *testing.T) {
	for _, tt := range []struct {
		name, actions string
		enrollment    watchers.Enrollment
	}{
		{"new configuration", `{"webhook":{"futureOption":"SECRET-SETTING"}}`, watchers.Enrollment{}},
		{"scheduled enrollment", `{}`, watchers.Enrollment{ScheduleCron: "0 8 * * 1"}},
		{"enrollment timezone", `{}`, watchers.Enrollment{ScheduleTimezone: "Europe/Helsinki"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := contractRaw()
			fixture := contractFixture(raw)
			fixture.EnrollmentDetails = map[string]watchers.Enrollment{raw.ID: tt.enrollment}
			data, err := json.Marshal(raw)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &fields))
			fields["actions"] = json.RawMessage(tt.actions)
			data, err = json.Marshal(fields)
			require.NoError(t, err)
			fixture.RawDetails = map[string]json.RawMessage{raw.ID: data}
			desired := minimalWatcher(t)
			desired.Notifications.Webhook.SigningSecret = &SecretInput{FromFile: filepath.Join(t.TempDir(), "must-not-read")}
			_, err = reconcile(t.Context(), clientForServer(t, fixture.ServeHTTP), desired, raw.ID)
			require.ErrorContains(t, err, "unrepresented configuration")
			assert.NotContains(t, err.Error(), "SECRET-SETTING")
			assert.NotContains(t, err.Error(), "cannot read")
			assert.Zero(t, fixture.MutationCalls())
		})
	}
}

func TestContractWebhookSourcesAndFullSecretOnlyPayload(t *testing.T) {
	raw := contractRaw()
	raw.Actions.Slack = &watchers.ChatAction{Enabled: true, Target: &watchers.ChatTarget{Type: "channel", ChannelID: "keep-slack"}, MinSeverity: "critical"}
	raw.Actions.MSTeams = &watchers.ChatAction{Enabled: true, Target: &watchers.ChatTarget{Type: "channel", ChannelID: "keep-teams"}, MinSeverity: "warning"}
	raw.Actions.Investigation = &watchers.InvestigationAction{Enabled: true, TeamNames: []string{"keep-team"}}
	raw.Actions.Webhook = &watchers.WebhookAction{Enabled: true, MinSeverity: "critical", SecureFields: &watchers.SecureFields{URL: true, HMACSecret: true, AuthorizationCredentials: true}}
	fixture := contractFixture(raw)
	client := clientForServer(t, fixture.ServeHTTP)
	desired := WatcherFromResponse(raw, false)
	t.Setenv("GCX_CONTRACT_ENDPOINT", "https://example.invalid/SECRET-ENDPOINT")
	t.Setenv("GCX_CONTRACT_HMAC", "SECRET-HMAC")
	path := filepath.Join(t.TempDir(), "credential")
	require.NoError(t, os.WriteFile(path, []byte("SECRET-BEARER\r\n"), 0600))
	desired.Notifications.Webhook.URL = &SecretInput{FromEnv: "GCX_CONTRACT_ENDPOINT"}
	desired.Notifications.Webhook.SigningSecret = &SecretInput{FromEnv: "GCX_CONTRACT_HMAC"}
	desired.Notifications.Webhook.BearerToken = &SecretInput{FromFile: path}
	authored, err := json.Marshal(desired)
	require.NoError(t, err)
	result, err := reconcile(t.Context(), client, desired, raw.ID)
	require.NoError(t, err)
	require.Equal(t, 1, fixture.MutationCalls())
	request := contractRequest(t, fixture, 0)
	contractCompletePayload(t, request)
	actions := testObject(t, request["actions"])
	webhook := testObject(t, actions["webhook"])
	assert.Equal(t, "https://example.invalid/SECRET-ENDPOINT", webhook["url"])
	assert.Equal(t, "Bearer", webhook["authorizationScheme"])
	assert.Equal(t, map[string]any{"hmacSecret": "SECRET-HMAC", "authorizationCredentials": "SECRET-BEARER"}, webhook["secureSettings"])
	assert.Equal(t, true, testObject(t, actions["slack"])["enabled"])
	assert.Equal(t, true, testObject(t, actions["msteams"])["enabled"])
	assert.Equal(t, true, testObject(t, actions["investigation"])["enabled"])
	stored, err := client.Get(t.Context(), raw.ID)
	require.NoError(t, err)
	assert.True(t, stored.Actions.Slack.Enabled)
	assert.True(t, stored.Actions.MSTeams.Enabled)
	assert.True(t, stored.Actions.Investigation.Enabled)
	assert.True(t, stored.Actions.Webhook.Enabled)
	assert.Equal(t, raw.DefinitionVersion, stored.DefinitionVersion)
	resultJSON, err := json.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(resultJSON), "SECRET-")
	assert.True(t, result.Notifications.Webhook.URL.Preserve)
	assert.True(t, result.Notifications.Webhook.BearerToken.Preserve)
	assert.True(t, result.Notifications.Webhook.SigningSecret.Preserve)
	after, err := json.Marshal(desired)
	require.NoError(t, err)
	assert.JSONEq(t, string(authored), string(after))
	// Supplying the same sources again is deliberate write intent, even though
	// every nonsecret setting and all presence markers are already equal.
	_, err = reconcile(t.Context(), client, desired, raw.ID)
	require.NoError(t, err)
	require.Equal(t, 2, fixture.MutationCalls())
	assert.JSONEq(t, string(fixture.MutationRequests()[0].Body), string(fixture.MutationRequests()[1].Body))
	contractNoLifecycle(t, fixture)
}

func TestContractSecretBearingMutationFailureIsRedacted(t *testing.T) {
	t.Setenv("GCX_CONTRACT_FAILURE_SECRET", "SECRET-SUBMITTED-CREDENTIAL")
	raw := contractRaw()
	fixture := contractFixture(raw)
	fixture.UpdateStatus = map[string]int{raw.ID: http.StatusBadRequest}
	fixture.ErrorBody = `{"message":"invalid credential SECRET-SUBMITTED-CREDENTIAL","request":{"hmacSecret":"SECRET-SUBMITTED-CREDENTIAL"}}`
	desired := minimalWatcher(t)
	desired.Notifications.Webhook.SigningSecret = &SecretInput{FromEnv: "GCX_CONTRACT_FAILURE_SECRET"}
	desired.AutomaticRecalibration.Enabled = true
	result, err := reconcile(t.Context(), clientForServer(t, fixture.ServeHTTP), desired, raw.ID)
	require.ErrorIs(t, err, watchers.ErrInvalidConfiguration)
	assert.Nil(t, result)
	assert.NotContains(t, err.Error(), "SECRET-SUBMITTED-CREDENTIAL")
	encoded, encodeErr := json.Marshal(err)
	require.NoError(t, encodeErr)
	assert.NotContains(t, string(encoded), "SECRET-SUBMITTED-CREDENTIAL")
	assert.Equal(t, 1, fixture.MutationCalls())
	contractNoLifecycle(t, fixture)
}

func TestContractWebhookClearAndSourceFailures(t *testing.T) {
	t.Run("clear stored disabled destination", func(t *testing.T) {
		raw := contractRaw()
		raw.Actions.Webhook = &watchers.WebhookAction{SecureFields: &watchers.SecureFields{URL: true, HMACSecret: true, AuthorizationCredentials: true}}
		fixture := contractFixture(raw)
		desired := WatcherFromResponse(raw, false)
		desired.Notifications.Webhook.URL = &SecretInput{Clear: true}
		desired.Notifications.Webhook.BearerToken = &SecretInput{Clear: true}
		desired.Notifications.Webhook.SigningSecret = &SecretInput{Clear: true}
		result, err := reconcile(t.Context(), clientForServer(t, fixture.ServeHTTP), desired, raw.ID)
		require.NoError(t, err)
		require.Equal(t, 1, fixture.MutationCalls())
		request := contractRequest(t, fixture, 0)
		contractCompletePayload(t, request)
		webhook := testObject(t, testObject(t, request["actions"])["webhook"])
		assert.Empty(t, webhook["url"])
		assert.Equal(t, map[string]any{"hmacSecret": "", "authorizationCredentials": ""}, webhook["secureSettings"])
		assert.Nil(t, result.Notifications.Webhook.URL)
		assert.Nil(t, result.Notifications.Webhook.BearerToken)
		assert.Nil(t, result.Notifications.Webhook.SigningSecret)
	})
	for _, tt := range []struct {
		name    string
		source  func(*testing.T) *SecretInput
		message string
	}{
		{"missing environment", func(t *testing.T) *SecretInput {
			t.Helper()
			t.Setenv("GCX_CONTRACT_MISSING", "")
			return &SecretInput{FromEnv: "GCX_CONTRACT_MISSING"}
		}, "missing or empty"},
		{"missing file", func(t *testing.T) *SecretInput {
			t.Helper()
			return &SecretInput{FromFile: filepath.Join(t.TempDir(), "missing")}
		}, "cannot read"},
		{"empty file", func(t *testing.T) *SecretInput {
			t.Helper()
			path := filepath.Join(t.TempDir(), "empty")
			require.NoError(t, os.WriteFile(path, []byte("\r\n"), 0600))
			return &SecretInput{FromFile: path}
		}, "missing or empty"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := contractRaw()
			fixture := contractFixture(raw)
			desired := minimalWatcher(t)
			desired.Notifications.Webhook.SigningSecret = tt.source(t)
			_, err := reconcile(t.Context(), clientForServer(t, fixture.ServeHTTP), desired, raw.ID)
			require.ErrorContains(t, err, tt.message)
			assert.Zero(t, fixture.MutationCalls())
		})
	}
	for _, field := range []string{"url", "bearer", "signing"} {
		t.Run("preserve on create "+field, func(t *testing.T) {
			fixture := &watcherstest.Server{AllowWrites: true}
			desired := minimalWatcher(t)
			switch field {
			case "url":
				desired.Notifications.Webhook.URL = &SecretInput{Preserve: true}
			case "bearer":
				desired.Notifications.Webhook.BearerToken = &SecretInput{Preserve: true}
			case "signing":
				desired.Notifications.Webhook.SigningSecret = &SecretInput{Preserve: true}
			}
			_, err := reconcile(t.Context(), clientForServer(t, fixture.ServeHTTP), desired, "")
			require.ErrorContains(t, err, "cannot preserve")
			assert.Zero(t, fixture.MutationCalls())
		})
	}
}

func TestContractURLWritesRequireEveryStoredCredential(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		url, bearer, signing *SecretInput
		expected             string
	}{
		{"supply URL omitted credentials", &SecretInput{FromEnv: "GCX_CONTRACT_ENDPOINT"}, nil, nil, "every saved"},
		{"supply URL preserved bearer", &SecretInput{FromEnv: "GCX_CONTRACT_ENDPOINT"}, &SecretInput{Preserve: true}, &SecretInput{Clear: true}, "every saved"},
		{"clear URL preserved signing", &SecretInput{Clear: true}, &SecretInput{Clear: true}, &SecretInput{Preserve: true}, "every saved"},
		{"clear URL omitted bearer", &SecretInput{Clear: true}, nil, &SecretInput{Clear: true}, "every saved"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GCX_CONTRACT_ENDPOINT", "")
			raw := contractRaw()
			raw.Actions.Webhook = &watchers.WebhookAction{Enabled: false, SecureFields: &watchers.SecureFields{URL: true, HMACSecret: true, AuthorizationCredentials: true}}
			fixture := contractFixture(raw)
			desired := WatcherFromResponse(raw, false)
			desired.Notifications.Webhook.URL = tt.url
			desired.Notifications.Webhook.BearerToken = tt.bearer
			desired.Notifications.Webhook.SigningSecret = tt.signing
			_, err := reconcile(t.Context(), clientForServer(t, fixture.ServeHTTP), desired, raw.ID)
			require.ErrorContains(t, err, tt.expected)
			assert.Zero(t, fixture.MutationCalls())
		})
	}
	t.Run("enabled endpoint cannot clear", func(t *testing.T) {
		raw := contractRaw()
		fixture := contractFixture(raw)
		desired := minimalWatcher(t)
		desired.Notifications.Webhook.Enabled = true
		desired.Notifications.Webhook.URL = &SecretInput{Clear: true}
		_, err := reconcile(t.Context(), clientForServer(t, fixture.ServeHTTP), desired, raw.ID)
		require.ErrorContains(t, err, "requires a source or preserve")
		assert.Zero(t, fixture.MutationCalls())
		assert.Zero(t, fixture.EnrollmentReads(raw.ID))
	})
}

func TestContractIndependentEnrollmentAndPartialReapply(t *testing.T) {
	for _, create := range []bool{false, true} {
		t.Run(map[bool]string{false: "update", true: "create"}[create], func(t *testing.T) {
			raw := contractRaw()
			fixture := contractFixture(raw)
			id := raw.ID
			if create {
				fixture.Current = nil
				id = ""
			}
			fixture.EnrollmentWriteStatus = map[string]int{raw.ID: 503, "00000000-0000-4000-8000-000000000001": 503}
			fixture.ErrorBody = `{"message":"SECRET-SERVER"}`
			desired := minimalWatcher(t)
			desired.Description = "Saved config"
			desired.AutomaticRecalibration.Enabled = true
			client := clientForServer(t, fixture.ServeHTTP)
			result, err := reconcile(t.Context(), client, desired, id)
			assert.Nil(t, result)
			var partial *PartialCompletionError
			require.ErrorAs(t, err, &partial)
			assert.NotEmpty(t, partial.Watcher.ServerID())
			assert.Equal(t, "example", partial.Watcher.GetResourceName())
			assert.Equal(t, "saved", partial.Watcher.MutationOutcome().Configuration)
			assert.Equal(t, "failed", partial.Watcher.MutationOutcome().Enrollment)
			assert.NotContains(t, err.Error(), "SECRET-SERVER")
			require.Equal(t, 2, fixture.MutationCalls())
			requests := fixture.MutationRequests()
			assert.False(t, strings.HasSuffix(requests[0].Path, "/auto-calibration"))
			assert.True(t, strings.HasSuffix(requests[1].Path, "/auto-calibration"))
			fixture.EnrollmentWriteStatus = nil
			result, err = reconcile(t.Context(), client, desired, partial.Watcher.ServerID())
			require.NoError(t, err)
			assert.Equal(t, "unchanged", result.MutationOutcome().Configuration)
			assert.Equal(t, "saved", result.MutationOutcome().Enrollment)
			require.Equal(t, 3, fixture.MutationCalls())
			assert.True(t, strings.HasSuffix(fixture.MutationRequests()[2].Path, "/auto-calibration"))
		})
	}
	t.Run("create disabled enrollment needs one request", func(t *testing.T) {
		fixture := &watcherstest.Server{AllowWrites: true}
		result, err := reconcile(t.Context(), clientForServer(t, fixture.ServeHTTP), minimalWatcher(t), "")
		require.NoError(t, err)
		assert.False(t, result.AutomaticRecalibration.Enabled)
		assert.Equal(t, 1, fixture.MutationCalls())
		assert.Equal(t, http.MethodPost, fixture.MutationRequests()[0].Method)
	})
	t.Run("enrollment only skips configuration", func(t *testing.T) {
		raw := contractRaw()
		fixture := contractFixture(raw)
		desired := WatcherFromResponse(raw, false)
		desired.AutomaticRecalibration.Enabled = true
		result, err := reconcile(t.Context(), clientForServer(t, fixture.ServeHTTP), desired, raw.ID)
		require.NoError(t, err)
		assert.Equal(t, "unchanged", result.MutationOutcome().Configuration)
		assert.Equal(t, "saved", result.MutationOutcome().Enrollment)
		require.Equal(t, 1, fixture.MutationCalls())
		assert.True(t, strings.HasSuffix(fixture.MutationRequests()[0].Path, "/auto-calibration"))
		assert.JSONEq(t, `{"enabled":true}`, string(fixture.MutationRequests()[0].Body))
	})
}

func TestContractReadbackFailureReportsSavedIdentityAndReapplyIsSafe(t *testing.T) {
	for _, stage := range []string{"detail", "enrollment"} {
		t.Run(stage, func(t *testing.T) {
			raw := contractRaw()
			fixture := contractFixture(raw)
			var fail atomic.Bool
			fail.Store(true)
			client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
				failingPath := testCollectionPath + "/" + raw.ID
				if stage == "enrollment" {
					failingPath += "/auto-calibration"
				}
				if r.Method == http.MethodGet && r.URL.Path == failingPath && fixture.MutationCalls() > 0 && fail.Load() {
					http.Error(w, "SECRET-READBACK", http.StatusInternalServerError)
					return
				}
				fixture.ServeHTTP(w, r)
			})
			desired := WatcherFromResponse(raw, false)
			desired.Description = "Saved"
			desired.AutomaticRecalibration.Enabled = true
			_, err := reconcile(t.Context(), client, desired, raw.ID)
			var partial *PartialCompletionError
			require.ErrorAs(t, err, &partial)
			assert.Equal(t, raw.ID, partial.Watcher.ServerID())
			assert.Equal(t, "saved", partial.Watcher.MutationOutcome().Configuration)
			assert.Equal(t, "saved", partial.Watcher.MutationOutcome().Enrollment)
			assert.Equal(t, "failed", partial.Watcher.MutationOutcome().Readback)
			assert.NotContains(t, err.Error(), "SECRET-READBACK")
			require.Equal(t, 2, fixture.MutationCalls())
			contractNoLifecycle(t, fixture)
			fail.Store(false)
			result, err := reconcile(t.Context(), client, desired, raw.ID)
			require.NoError(t, err)
			assert.False(t, result.MutationOutcome().Changed)
			assert.Equal(t, 2, fixture.MutationCalls())
		})
	}
}
