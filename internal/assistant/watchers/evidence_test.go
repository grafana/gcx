package watchers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/assistant/watchers/watcherstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigurationEvidence(t *testing.T) {
	for _, tt := range []struct {
		name, body  string
		unsupported bool
	}{
		{"calibrated content", `{"queries":[{"future":"SENTINEL"}],"calibrationContext":"guidance","staticLogPolicies":{"query":{"future":"policy"}},"shortTermMemory":"learned","ongoingIssues":[{"text":"learned"}],"futureRuntimeObservation":{"value":"runtime"}}`, false},
		{"deprecated", `{"temporary":{"endsAt":"2027-01-01T00:00:00Z"},"actions":{"irm":{"enabled":true,"future":"legacy"}}}`, false},
		{"implicit defaults", `{"actions":{"slack":{"enabled":false,"target":{"type":"channel"}},"msteams":{"enabled":false,"target":{"type":"channel"}},"webhook":{"authorizationScheme":"Bearer"},"alerting":{"minSeverity":"warning","routingTree":""}}}`, false},
		{"default route alias", `{"actions":{"alerting":{"routingTree":"default"}}}`, false},
		{"future modeled values", `{"sensitivity":"future","actions":{"slack":{"minSeverity":"future"},"msteams":{"minSeverity":"future"},"webhook":{"minSeverity":"future"}}}`, false},
		{"disabled slack dm", `{"actions":{"slack":{"enabled":false,"target":{"type":"dm","userId":"user"}}}}`, true},
		{"slack workspace", `{"actions":{"slack":{"target":{"type":"channel","teamId":"team"}}}}`, true},
		{"target future", `{"actions":{"msteams":{"target":{"future":"SENTINEL"}}}}`, true},
		{"disabled alerting severity", `{"actions":{"alerting":{"enabled":false,"minSeverity":"critical"}}}`, true},
		{"alerting routing", `{"actions":{"alerting":{"routingTree":"custom"}}}`, true},
		{"legacy routing", `{"actions":{"alerting":{"labels":{"__grafana_managed_route__":"custom"}}}}`, true},
		{"webhook scheme", `{"actions":{"webhook":{"authorizationScheme":"future"}}}`, true},
		{"future webhook credential", `{"actions":{"webhook":{"secureFields":{"futureCredential":true}}}}`, true},
		{"unknown action", `{"actions":{"future":{"enabled":true,"value":"SENTINEL"}}}`, true},
		{"empty unknown action", `{"actions":{"future":{"enabled":false,"value":""}}}`, false},
		{"unknown stop", `{"autoStop":{"future":true}}`, true},
		{"unknown investigation", `{"actions":{"investigation":{"future":true}}}`, true},
		{"input secrets ignored", `{"actions":{"webhook":{"url":"SENTINEL","urlDisplay":"SENTINEL","secureSettings":{"hmacSecret":"SENTINEL"},"secureFields":{"url":true}}}}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var item watchers.Watcher
			require.NoError(t, json.Unmarshal([]byte(tt.body), &item))
			categories := item.UnsupportedConfiguration()
			assert.Equal(t, tt.unsupported, len(categories) > 0)
			for _, category := range categories {
				assert.NotContains(t, category, "SENTINEL")
			}
			if tt.name == "input secrets ignored" {
				encoded, err := json.Marshal(item)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), "SENTINEL")
				assert.True(t, item.Actions.Webhook.SecureFields.URL)
			}
		})
	}
}

func TestCompatibilityAliasesNormalizeWithoutOverwritingCanonical(t *testing.T) {
	var legacy watchers.Watcher
	require.NoError(t, json.Unmarshal([]byte(`{"temporary":{"endsAt":"2027-01-01T00:00:00Z"},"actions":{"alerting":{"labels":{"team":"example"}}}}`), &legacy))
	assert.Equal(t, map[string]string{"team": "example"}, legacy.Labels)
	require.NotNil(t, legacy.AutoStop)
	require.NotNil(t, legacy.AutoStop.PauseAt)
	assert.Equal(t, 2027, legacy.AutoStop.PauseAt.Year())
	var canonical watchers.Watcher
	require.NoError(t, json.Unmarshal([]byte(`{"labels":{},"autoStop":{},"temporary":{"endsAt":"2027-01-01T00:00:00Z"},"actions":{"alerting":{"labels":{"team":"example"}}}}`), &canonical))
	assert.Empty(t, canonical.Labels)
	require.NotNil(t, canonical.AutoStop)
	assert.Nil(t, canonical.AutoStop.PauseAt)
}

func TestEnrollmentEvidenceSurvivesReads(t *testing.T) {
	for _, tt := range []struct {
		name, cron, timezone string
		unsupported          bool
	}{
		{"absent", "", "", false},
		{"default", "0 9 * * 1", "UTC", false},
		{"custom cron", "0 8 * * 1", "UTC", true},
		{"custom timezone", "0 9 * * 1", "Europe/Helsinki", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := &watcherstest.Server{EnrollmentDetails: map[string]watchers.Enrollment{"id": {Enabled: true, ScheduleCron: tt.cron, ScheduleTimezone: tt.timezone}}}
			item, err := mutationClient(t, fixture).Enrollment(t.Context(), "id")
			require.NoError(t, err)
			assert.True(t, item.Enabled)
			assert.Equal(t, tt.cron, item.ScheduleCron)
			assert.Equal(t, tt.timezone, item.ScheduleTimezone)
			assert.Equal(t, tt.unsupported, len(item.UnsupportedConfiguration()) > 0)
			assert.Zero(t, fixture.MutationCalls())
		})
	}
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeData(t, w, map[string]any{"enabled": false, "watcherId": "id", "nextRunAt": "2027-01-01T00:00:00Z", "futureConfig": "SENTINEL"})
	})
	item, err := client.Enrollment(t.Context(), "id")
	require.NoError(t, err)
	assert.Equal(t, []string{"automatic recalibration settings"}, item.UnsupportedConfiguration())
	encoded, err := json.Marshal(item)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "SENTINEL")
}
