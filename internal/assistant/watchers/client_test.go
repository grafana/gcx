package watchers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const basePath = "/api/plugins/grafana-assistant-app/resources/api/v1/watcher-agents"

func newTestClient(t *testing.T, handle http.HandlerFunc) *watchers.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, int64(0), r.ContentLength)
		handle(w, r)
	}))
	t.Cleanup(server.Close)
	base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
	require.NoError(t, err)
	return watchers.NewClient(base)
}

func writeData(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": data}))
}

func TestIsServerIDRequiresCanonicalRepresentation(t *testing.T) {
	for _, tt := range []struct {
		ref string
		id  bool
	}{
		{ref: "00000000-0000-4000-8000-00000000000a", id: true},
		{ref: "00000000-0000-4000-8000-00000000000A"},
		{ref: "0000000000004000800000000000000a"},
		{ref: "{00000000-0000-4000-8000-00000000000a}"},
		{ref: "urn:uuid:00000000-0000-4000-8000-00000000000a"},
		{ref: " 00000000-0000-4000-8000-00000000000a "},
		{ref: "00000000-0000-4000-8000-00000000000x"},
		{ref: "id-1"},
		{ref: "checkout-health"},
		{ref: ""},
	} {
		t.Run(tt.ref, func(t *testing.T) {
			assert.Equal(t, tt.id, watchers.IsServerID(tt.ref))
		})
	}
}

func TestListAllExhaustsCursorThroughEmptyPage(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(strconv.FormatBool(archived), func(t *testing.T) {
			var cursors []string
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, basePath, r.URL.Path)
				assert.Equal(t, "100", r.URL.Query().Get("page_size"))
				assert.Equal(t, strconv.FormatBool(archived), r.URL.Query().Get("archived"))
				cursor := r.URL.Query().Get("cursor")
				cursors = append(cursors, cursor)
				switch cursor {
				case "":
					writeData(t, w, map[string]any{"agents": []watchers.Watcher{{ID: "a", Name: "same title"}}, "nextCursor": "a+b/="})
				case "a+b/=":
					writeData(t, w, map[string]any{"agents": []watchers.Watcher{}, "nextCursor": "last"})
				case "last":
					writeData(t, w, map[string]any{"agents": []watchers.Watcher{{ID: "b", Name: "same title"}}})
				default:
					t.Errorf("unexpected cursor %q", cursor)
				}
			})
			items, err := client.ListAll(t.Context(), archived)
			require.NoError(t, err)
			require.Len(t, items, 2)
			assert.Equal(t, []string{"", "a+b/=", "last"}, cursors)
			assert.Equal(t, "a", items[0].ID)
			assert.Equal(t, "b", items[1].ID)
		})
	}
}

func TestListAllEmptyIsInitialized(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeData(t, w, map[string]any{"agents": []watchers.Watcher{}})
	})
	items, err := client.ListAll(t.Context(), false)
	require.NoError(t, err)
	require.NotNil(t, items)
	assert.Empty(t, items)
}

func TestListAllRetainsFirstObservationPerServerID(t *testing.T) {
	for _, tt := range []struct {
		name  string
		pages [][]watchers.Watcher
	}{
		{
			name: "overlapping pages",
			pages: [][]watchers.Watcher{
				{{ID: "a", Name: "first observation", Prompt: "first prompt"}},
				{{ID: "a", Name: "later observation", Prompt: "changed prompt"}, {ID: "b", Name: "unique"}},
			},
		},
		{
			name: "duplicate page followed by unique resource",
			pages: [][]watchers.Watcher{
				{{ID: "a", Name: "first observation", Prompt: "first prompt"}, {ID: "a", Name: "same page duplicate"}},
				{{ID: "a", Name: "later observation"}},
				{{ID: "b", Name: "unique"}},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				cursor := ""
				if calls > 0 {
					cursor = strconv.Itoa(calls)
				}
				assert.Equal(t, cursor, r.URL.Query().Get("cursor"))
				if !assert.Less(t, calls, len(tt.pages)) {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				page := tt.pages[calls]
				calls++
				next := ""
				if calls < len(tt.pages) {
					next = strconv.Itoa(calls)
				}
				writeData(t, w, map[string]any{"agents": page, "nextCursor": next})
			})
			items, err := client.ListAll(t.Context(), false)
			require.NoError(t, err)
			require.Len(t, items, 2)
			assert.Equal(t, "a", items[0].ID)
			assert.Equal(t, "first observation", items[0].Name)
			assert.Equal(t, "first prompt", items[0].Prompt)
			assert.Equal(t, "b", items[1].ID)
			assert.Equal(t, "unique", items[1].Name)
			assert.Equal(t, len(tt.pages), calls)
		})
	}
}

func TestListAllRejectsMalformedPagesAndIdentities(t *testing.T) {
	for _, body := range []string{
		`{"data":{}}`, `{"data":{"agents":null}}`, `{"data":{"agents":{}}}`,
		`{"data":{"agents":[{"id":"","name":"example"}]}}`,
		`{"data":{"agents":[{"id":"example","name":" "}]}}`,
		`{"data":{"agents":[{"id":"example","name":"valid"},{"name":"missing id"}]}}`,
	} {
		t.Run(body, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
			items, err := client.ListAll(t.Context(), false)
			require.Error(t, err)
			assert.Nil(t, items)
		})
	}
}

func TestListAllNeverReturnsPartialSuccess(t *testing.T) {
	for _, failure := range []string{"server", "cursor-cycle", "decode"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls == 1 {
					writeData(t, w, map[string]any{"agents": []watchers.Watcher{{ID: "a", Name: "first"}}, "nextCursor": "first"})
					return
				}
				switch failure {
				case "server":
					w.WriteHeader(http.StatusInternalServerError)
				case "cursor-cycle":
					writeData(t, w, map[string]any{"agents": []watchers.Watcher{}, "nextCursor": "first"})
				case "decode":
					_, _ = w.Write([]byte("bad json"))
				}
			})
			items, err := client.ListAll(t.Context(), false)
			require.Error(t, err)
			assert.Nil(t, items)
			assert.Equal(t, 2, calls)
		})
	}
}

func TestListAllCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	calls := 0
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		cancel()
		writeData(t, w, map[string]any{"agents": []watchers.Watcher{{ID: "a", Name: "first"}}, "nextCursor": "more"})
	})
	items, err := client.ListAll(ctx, false)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, items)
	assert.Equal(t, 1, calls)
}

func TestGetMapsConfigurationAndObservationsWithoutSecrets(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, basePath+"/id%2Fwith%3Fchars", r.URL.EscapedPath())
		_, _ = w.Write([]byte(`{"data":{"id":"id/with?chars","name":"Example","description":"description","prompt":"monitor","status":"paused","archivedAt":"2030-01-01T00:00:00Z","triggerIntervalSeconds":600,"sensitivity":"relaxed","disableDecisionSkip":true,"datasourceUids":["source"],"labels":{"service":"example"},"autoStop":{"pauseAt":"2030-01-02T00:00:00Z","archive":true},"actions":{"slack":{"enabled":true,"target":{"channelId":"channel"},"minSeverity":"critical"},"msteams":{"enabled":false,"target":{"channelId":"teams"}},"alerting":{"enabled":true},"investigation":{"enabled":true,"teamNames":["example"]},"webhook":{"enabled":false,"minSeverity":"critical","url":"SECRET-URL","urlDisplay":"REDACTED-URL","secureSettings":{"hmacSecret":"SECRET-SIGNATURE","authorizationCredentials":"SECRET-TOKEN"},"secureFields":{"url":true,"hmacSecret":true,"authorizationCredentials":false}}},"queries":[{"id":"check","type":"new_type","expr":"expression","newParameter":{"window":42}}],"calibrationContext":"context","lastRunAt":"2030-01-01T01:00:00Z","lastRunAssessment":"warning","nextRunAt":"2030-01-01T02:00:00Z","tokenConsumption":{"averagePerRun":45,"estimatedPerHour":270,"sampleSize":3},"calibratedAt":"2030-01-01T00:00:00Z","createdBy":"creator","createdAt":"2030-01-01T00:00:00Z","updatedAt":"2030-01-01T01:00:00Z","definitionVersion":7}}`))
	})
	item, err := client.Get(t.Context(), "id/with?chars")
	require.NoError(t, err)
	assert.Equal(t, "Example", item.Name)
	assert.Equal(t, "description", item.Description)
	assert.Equal(t, "monitor", item.Prompt)
	assert.Equal(t, "paused", item.Status)
	assert.Equal(t, 600, item.TriggerIntervalSeconds)
	assert.Equal(t, "relaxed", item.Sensitivity)
	assert.True(t, item.DisableDecisionSkip)
	assert.Equal(t, []string{"source"}, item.DatasourceUIDs)
	assert.Equal(t, map[string]string{"service": "example"}, item.Labels)
	require.NotNil(t, item.AutoStop)
	assert.True(t, item.AutoStop.Archive)
	assert.Equal(t, "2030-01-02T00:00:00Z", item.AutoStop.PauseAt.Format(time.RFC3339))
	assert.Equal(t, "channel", item.Actions.Slack.Target.ChannelID)
	assert.Equal(t, "critical", item.Actions.Slack.MinSeverity)
	assert.Equal(t, "teams", item.Actions.MSTeams.Target.ChannelID)
	assert.True(t, item.Actions.Alerting.Enabled)
	assert.Equal(t, []string{"example"}, item.Actions.Investigation.TeamNames)
	assert.True(t, item.Actions.Webhook.SecureFields.URL)
	assert.True(t, item.Actions.Webhook.SecureFields.HMACSecret)
	assert.False(t, item.Actions.Webhook.SecureFields.AuthorizationCredentials)
	assert.Equal(t, map[string]any{"window": float64(42)}, item.Queries[0]["newParameter"])
	assert.Equal(t, "context", item.CalibrationContext)
	assert.Equal(t, "warning", item.LastRunAssessment)
	assert.Equal(t, int64(45), item.TokenConsumption.AveragePerRun)
	assert.Equal(t, int64(270), item.TokenConsumption.EstimatedPerHour)
	assert.Equal(t, 3, item.TokenConsumption.SampleSize)
	assert.Equal(t, "creator", item.CreatedBy)
	assert.Equal(t, int64(7), item.DefinitionVersion)
	encoded, err := json.Marshal(item)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "SECRET")
	assert.NotContains(t, string(encoded), "REDACTED")
}

func TestSupplementaryReads(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case basePath + "/example/auto-calibration":
			writeData(t, w, map[string]any{"enabled": false})
		case basePath + "/example/initial-calibration":
			writeData(t, w, map[string]any{"status": "needs_input", "message": "Continue in Grafana", "startedAt": "2030-01-01T00:00:00Z", "activity": map[string]any{"chatId": "chat", "taskId": "task"}})
		default:
			t.Errorf("unexpected read %s", r.URL.Path)
		}
	})
	enrollment, err := client.Enrollment(t.Context(), "example")
	require.NoError(t, err)
	assert.False(t, enrollment.Enabled)
	calibration, err := client.Calibration(t.Context(), "example")
	require.NoError(t, err)
	assert.Equal(t, "needs_input", calibration.Status)
	assert.Equal(t, "Continue in Grafana", calibration.Message)
	assert.Equal(t, "chat", calibration.Activity.ChatID)
	assert.Equal(t, "task", calibration.Activity.TaskID)
	assert.Equal(t, "2030-01-01T00:00:00Z", calibration.StartedAt.Format(time.RFC3339))
}

func TestErrorsRetainClassification(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		collection bool
		want       error
	}{
		{"missing collection", 404, "", true, watchers.ErrCapabilityUnavailable},
		{"missing ID", 404, "", false, watchers.ErrNotFound},
		{"unauthorized", 401, "", false, watchers.ErrPermissionDenied},
		{"denied collection", 403, `{"name":"NOT_IMPLEMENTED"}`, true, watchers.ErrPermissionDenied},
		{"missing supplementary capability", 501, "", false, watchers.ErrCapabilityUnavailable},
		{"structured unavailable", 500, `{"name":"NOT_IMPLEMENTED","message":"private detail"}`, false, watchers.ErrCapabilityUnavailable},
		{"server failure", 500, "SECRET BODY", false, nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			var err error
			if tt.collection {
				_, err = client.ListAll(t.Context(), false)
			} else {
				_, err = client.Get(t.Context(), "example")
			}
			require.Error(t, err)
			_, plain := err.(*watchers.APIError) //nolint:errorlint // This contract requires a direct APIError, without wrappers.
			require.True(t, plain, "client must return the API error directly")
			var apiErr *watchers.APIError
			require.ErrorAs(t, fmt.Errorf("wrapped: %w", err), &apiErr)
			assert.Equal(t, tt.status, apiErr.HTTPStatusCode())
			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
			} else {
				require.NoError(t, errors.Unwrap(apiErr))
			}
			assert.NotContains(t, apiErr.APIUserMessage(), "private detail")
			assert.NotContains(t, apiErr.APIUserMessage(), "SECRET BODY")
		})
	}
}

func TestMalformedSuccessIsNotConfiguration(t *testing.T) {
	for _, body := range []string{"invalid", `{}`, `{"data":null}`, `{"data":{}}`} {
		t.Run(body, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
			_, err := client.Get(t.Context(), "example")
			require.Error(t, err)
			_, err = client.Enrollment(t.Context(), "example")
			require.Error(t, err)
			_, err = client.Calibration(t.Context(), "example")
			require.Error(t, err)
		})
	}
}

func TestMissingIDDoesNoIO(t *testing.T) {
	client := newTestClient(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("unexpected request") })
	_, err := client.Get(t.Context(), " ")
	require.Error(t, err)
	_, err = client.Enrollment(t.Context(), "")
	require.Error(t, err)
	_, err = client.Calibration(t.Context(), "\t")
	require.Error(t, err)
}
