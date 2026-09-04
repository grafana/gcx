package alert_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const historianNamespace = "default"

// newHistorianFixture starts a mock server for the historian API and returns a
// loader whose namespace is set, so the client builds a realistic request path.
func newHistorianFixture(t *testing.T, handler http.HandlerFunc) fakeGrafanaConfigLoader {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return fakeGrafanaConfigLoader{cfg: config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: historianNamespace,
	}}
}

// sampleEntries is the canonical notification-history fixture: one successful
// firing notification and one failed delivery carrying an error.
func sampleEntries() []alert.NotificationEntry {
	return []alert.NotificationEntry{
		{
			Timestamp:   time.Date(2026, 8, 28, 10, 16, 49, 0, time.UTC),
			UUID:        "d97e1040-7be1-41e4-811e-90877f5cdb87",
			Receiver:    "slack-tempo-alerts-prod",
			Integration: "slack",
			Status:      alert.NotificationStatusFiring,
			Outcome:     alert.NotificationOutcomeSuccess,
			GroupLabels: map[string]string{"alertname": "Tempo Writes", "cluster": "prod-eu-west-2"},
			RuleUIDs:    []string{"dfh3yvo76tedda"},
			AlertCount:  12,
			Duration:    185734320,
		},
		{
			Timestamp:   time.Date(2026, 8, 28, 10, 17, 1, 0, time.UTC),
			UUID:        "00ba190c-29d3-440a-a9df-f2a02a70af7f",
			Receiver:    "autogen-contact-point-default",
			Integration: "webhook",
			Status:      alert.NotificationStatusFiring,
			Outcome:     alert.NotificationOutcomeError,
			AlertCount:  5889,
			Error:       "webhook response status 413 Request Entity Too Large",
			Duration:    3314349050,
		},
	}
}

func serveNotifications(t *testing.T, entries []alert.NotificationEntry, captured *alert.NotificationQueryRequest, path *string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*path = r.URL.Path
		if captured != nil {
			body, err := io.ReadAll(r.Body)
			if !assert.NoError(t, err) {
				return
			}
			assert.NoError(t, json.Unmarshal(body, captured))
		}
		writeJSON(w, alert.NotificationQueryResponse{Entries: entries})
	}
}

func TestNotificationHistoryListRequest(t *testing.T) {
	setAgentMode(t, true)

	var captured alert.NotificationQueryRequest
	var path string
	loader := newHistorianFixture(t, serveNotifications(t, sampleEntries(), &captured, &path))

	stdout, _, err := runCmdSplit(t, alert.NewNotificationHistoryListCommandForTest(loader),
		[]string{
			"list",
			"--from", "2026-08-27T00:00:00Z",
			"--to", "2026-08-28T00:00:00Z",
			"--limit", "25",
			"--status", "firing",
			"--outcome", "error",
			"--receiver", "my-cp",
			"--rule-uid", "rule-1",
		}, "")
	require.NoError(t, err)

	assert.Equal(t, "/apis/historian.alerting.grafana.app/v0alpha1/namespaces/default/notification/query", path)
	assert.Equal(t, time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC), captured.From.UTC())
	assert.Equal(t, time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC), captured.To.UTC())
	assert.Equal(t, int64(25), captured.Limit)
	assert.Equal(t, "firing", captured.Status)
	assert.Equal(t, "error", captured.Outcome)
	assert.Equal(t, "my-cp", captured.Receiver)
	assert.Equal(t, "rule-1", captured.RuleUID)

	doc := decodeSingleJSONDocument(t, stdout)
	items, ok := doc.([]any)
	require.True(t, ok, "notification history result should be a JSON array, got %T", doc)
	require.Len(t, items, 2)
}

func TestNotificationHistoryListHumanTable(t *testing.T) {
	setAgentMode(t, false)

	entries := sampleEntries()
	var path string
	loader := newHistorianFixture(t, serveNotifications(t, entries, nil, &path))

	stdout, _, err := runCmdSplit(t, alert.NewNotificationHistoryListCommandForTest(loader),
		[]string{"list"}, "")
	require.NoError(t, err)

	var want bytes.Buffer
	require.NoError(t, (&alert.NotificationHistoryTableCodec{}).Encode(&want, entries))
	assert.Equal(t, want.String(), stdout)
}

func TestNotificationHistoryListDisabled(t *testing.T) {
	setAgentMode(t, false)

	loader := newHistorianFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Notification history query whilst disabled"}`))
	})

	_, _, err := runCmdSplit(t, alert.NewNotificationHistoryListCommandForTest(loader),
		[]string{"list"}, "")
	require.ErrorIs(t, err, alert.ErrNotificationHistoryDisabled)
}

func TestNotificationHistoryAlerts(t *testing.T) {
	setAgentMode(t, true)

	var capturedUUID string
	var path string
	loader := newHistorianFixture(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		var req alert.NotificationAlertsRequest
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			return
		}
		assert.NoError(t, json.Unmarshal(body, &req))
		capturedUUID = req.UUID
		writeJSON(w, alert.NotificationAlertsResponse{Alerts: []alert.NotificationAlert{
			{
				Status:   "firing",
				Labels:   map[string]string{"alertname": "Tempo Writes"},
				StartsAt: time.Date(2026, 8, 28, 10, 13, 40, 0, time.UTC),
			},
		}})
	})

	stdout, _, err := runCmdSplit(t, alert.NewNotificationHistoryAlertsCommandForTest(loader),
		[]string{"alerts", "--uuid", "d97e1040-7be1-41e4-811e-90877f5cdb87"}, "")
	require.NoError(t, err)

	assert.Equal(t, "/apis/historian.alerting.grafana.app/v0alpha1/namespaces/default/notifications/queryalerts", path)
	assert.Equal(t, "d97e1040-7be1-41e4-811e-90877f5cdb87", capturedUUID)

	doc := decodeSingleJSONDocument(t, stdout)
	items, ok := doc.([]any)
	require.True(t, ok, "alerts result should be a JSON array, got %T", doc)
	require.Len(t, items, 1)
}

func TestNotificationHistoryAlertsRequiresUUID(t *testing.T) {
	setAgentMode(t, false)

	loader := newHistorianFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call without uuid: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, _, err := runCmdSplit(t, alert.NewNotificationHistoryAlertsCommandForTest(loader),
		[]string{"alerts"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--uuid is required")
}
