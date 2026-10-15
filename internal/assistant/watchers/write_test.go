package watchers_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/assistant/watchers/watcherstest"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func mutationClient(t *testing.T, handler http.Handler) *watchers.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
	require.NoError(t, err)
	return watchers.NewClient(base)
}

func completeRequest() *watchers.ConfigurationRequest {
	return &watchers.ConfigurationRequest{
		Name: new("Example"), Description: new(""), Prompt: new("Watch errors"),
		TriggerIntervalSeconds: new(600), Sensitivity: new("future-value"), DisableDecisionSkip: new(false),
		DatasourceUIDs: new([]string{}), Labels: new(map[string]string{}), AutoStop: &watchers.AutoStop{},
		Actions: &watchers.WriteActions{
			Slack: &watchers.ChatAction{MinSeverity: "warning"}, MSTeams: &watchers.ChatAction{MinSeverity: "warning"},
			Alerting:      &watchers.AlertingAction{MinSeverity: "warning", RoutingTree: new("")},
			Investigation: &watchers.InvestigationAction{TeamNames: []string{}},
			Webhook:       &watchers.WebhookWriteAction{Enabled: new(false), MinSeverity: new("warning"), URL: new("SENTINEL-ENDPOINT"), SecureSettings: &watchers.WebhookSecureSettings{HMACSecret: new("SENTINEL-HMAC"), AuthorizationCredentials: new("SENTINEL-TOKEN")}},
		},
	}
}

func TestWriteTransportAndFixture(t *testing.T) {
	fixture := &watcherstest.Server{AllowWrites: true}
	client := mutationClient(t, fixture)
	request := completeRequest()
	created, err := client.Create(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, "draft", created.Status)
	assert.Equal(t, int64(1), created.DefinitionVersion)
	assert.True(t, created.Actions.Webhook.SecureFields.URL)
	assert.True(t, created.Actions.Webhook.SecureFields.HMACSecret)
	encoded, err := json.Marshal(created)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "SENTINEL")
	request.Name = new("Renamed")
	request.Actions.Webhook.URL = new("")
	updated, err := client.Update(t.Context(), created.ID, request)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.False(t, updated.Actions.Webhook.SecureFields.URL)
	enrollment, err := client.SetEnrollment(t.Context(), created.ID, true)
	require.NoError(t, err)
	assert.True(t, enrollment.Enabled)
	require.NoError(t, client.Delete(t.Context(), created.ID))
	_, err = client.Get(t.Context(), created.ID)
	require.ErrorIs(t, err, watchers.ErrNotFound)
	assert.Equal(t, 4, fixture.MutationCalls())
	requests := fixture.MutationRequests()
	require.Len(t, requests, 4)
	assert.Equal(t, http.MethodPost, requests[0].Method)
	assert.Equal(t, basePath, requests[0].Path)
	assert.JSONEq(t, `{"name":"Example","description":"","prompt":"Watch errors","triggerIntervalSeconds":600,"sensitivity":"future-value","disableDecisionSkip":false,"datasourceUids":[],"labels":{},"autoStop":{},"actions":{"slack":{"enabled":false,"target":null,"minSeverity":"warning"},"msteams":{"enabled":false,"target":null,"minSeverity":"warning"},"alerting":{"enabled":false,"minSeverity":"warning","routingTree":""},"investigation":{"enabled":false,"teamNames":[]},"webhook":{"enabled":false,"minSeverity":"warning","url":"SENTINEL-ENDPOINT","secureSettings":{"hmacSecret":"SENTINEL-HMAC","authorizationCredentials":"SENTINEL-TOKEN"}}}}`, string(requests[0].Body))
	assert.Equal(t, http.MethodPut, requests[1].Method)
	assert.Equal(t, basePath+"/"+created.ID, requests[1].Path)
	assert.JSONEq(t, `{"enabled":true}`, string(requests[2].Body))
	assert.Equal(t, basePath+"/"+created.ID+"/auto-calibration", requests[2].Path)
	assert.Equal(t, http.MethodDelete, requests[3].Method)
	assert.Empty(t, requests[3].Body)
}

func TestSecretOnlyRequestOmitsConfiguration(t *testing.T) {
	fixture := &watcherstest.Server{AllowWrites: true, Current: [][]watchers.Watcher{{{ID: "id", Name: "Example", DefinitionVersion: 3}}}}
	client := mutationClient(t, fixture)
	request := &watchers.ConfigurationRequest{Actions: &watchers.WriteActions{Webhook: &watchers.WebhookWriteAction{SecureSettings: &watchers.WebhookSecureSettings{HMACSecret: new("SENTINEL")}}}}
	updated, err := client.Update(t.Context(), "id", request)
	require.NoError(t, err)
	assert.Equal(t, int64(3), updated.DefinitionVersion)
	assert.JSONEq(t, `{"actions":{"webhook":{"secureSettings":{"hmacSecret":"SENTINEL"}}}}`, string(fixture.MutationRequests()[0].Body))
	encoded, err := json.Marshal(updated)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "SENTINEL")
}

func TestWriteFailuresAreClassifiedAndRedacted(t *testing.T) {
	for _, tt := range []struct {
		name     string
		status   int
		expected error
		body     string
		reason   string
	}{
		{"denied", 403, watchers.ErrPermissionDenied, `{"message":"SENTINEL"}`, ""},
		{"missing", 404, watchers.ErrNotFound, `{"message":"SENTINEL"}`, ""},
		{"unavailable", 501, watchers.ErrCapabilityUnavailable, `{"message":"SENTINEL"}`, ""},
		{"invalid", 400, watchers.ErrInvalidConfiguration, `{"message":"SENTINEL"}`, ""},
		{"invalid shape", 422, watchers.ErrInvalidConfiguration, `{"message":"SENTINEL"}`, ""},
		{"archived", 409, watchers.ErrConflict, `{"error":"invalid state: restore the archived watcher first SENTINEL"}`, "restore the archived Watcher"},
		{"calibrating", 409, watchers.ErrConflict, `{"message":"finish or cancel calibration before updating the watcher SENTINEL"}`, "cancel calibration in Grafana"},
		{"conflict", 409, watchers.ErrConflict, `{"message":"SENTINEL"}`, ""},
		{"server", 500, nil, `{"message":"SENTINEL"}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := &watcherstest.Server{AllowWrites: true, CreateStatus: tt.status, UpdateStatus: map[string]int{"id": tt.status}, DeleteStatus: map[string]int{"id": tt.status}, EnrollmentWriteStatus: map[string]int{"id": tt.status}, ErrorBody: tt.body}
			client := mutationClient(t, fixture)
			calls := []func() error{
				func() error { _, err := client.Create(t.Context(), completeRequest()); return err },
				func() error { _, err := client.Update(t.Context(), "id", completeRequest()); return err },
				func() error { return client.Delete(t.Context(), "id") },
				func() error { _, err := client.SetEnrollment(t.Context(), "id", false); return err },
			}
			for _, call := range calls {
				err := call()
				require.Error(t, err)
				if tt.expected != nil {
					require.ErrorIs(t, err, tt.expected)
				}
				var apiErr *watchers.APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, tt.status, apiErr.HTTPStatusCode())
				assert.NotContains(t, err.Error(), "SENTINEL")
				if tt.reason != "" {
					assert.Contains(t, err.Error(), tt.reason)
				}
				encoded, encodeErr := json.Marshal(err)
				require.NoError(t, encodeErr)
				assert.NotContains(t, string(encoded), "SENTINEL")
			}
			assert.Equal(t, 4, fixture.MutationCalls())
		})
	}
}

func TestWriteResponseFailuresNeverEchoPayload(t *testing.T) {
	for _, body := range []string{`SENTINEL`, `{"data":null}`, `{"data":{}}`, `{"data":{"id":"id","name":123,"url":"SENTINEL"}}`} {
		t.Run(body, func(t *testing.T) {
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, body) }))
			_, err := client.Update(t.Context(), "id", completeRequest())
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "SENTINEL")
		})
	}
}

type failingWriteTransport struct{ cause error }

func (t failingWriteTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.cause
}

func TestWriteTransportFailuresRetainCause(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
	}{
		{"DNS", &net.DNSError{Err: "no such host", Name: "example.invalid", IsNotFound: true}},
		{"TLS", x509.UnknownAuthorityError{}},
		{"closed connection", io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{
				Host: "https://example.invalid",
				WrapTransport: func(http.RoundTripper) http.RoundTripper {
					return failingWriteTransport{cause: tc.cause}
				},
			}})
			require.NoError(t, err)
			client := watchers.NewClient(base)
			for _, call := range []func() error{
				func() error { _, err := client.Create(t.Context(), completeRequest()); return err },
				func() error { _, err := client.Update(t.Context(), "id", completeRequest()); return err },
				func() error { return client.Delete(t.Context(), "id") },
				func() error { _, err := client.SetEnrollment(t.Context(), "id", false); return err },
			} {
				err := call()
				require.ErrorIs(t, err, tc.cause)
				assert.Contains(t, err.Error(), tc.cause.Error())
				assert.NotContains(t, err.Error(), "SENTINEL")
			}
		})
	}
}

func TestWriteInputFailuresSendNothing(t *testing.T) {
	fixture := &watcherstest.Server{AllowWrites: true}
	client := mutationClient(t, fixture)
	_, err := client.Create(t.Context(), nil)
	require.Error(t, err)
	_, err = client.Update(t.Context(), "", completeRequest())
	require.Error(t, err)
	require.Error(t, client.Delete(t.Context(), ""))
	_, err = client.SetEnrollment(t.Context(), "", false)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.Update(ctx, "id", completeRequest())
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, fixture.MutationCalls())
}

func TestReadResponseParseFailuresDoNotEchoSecrets(t *testing.T) {
	for _, field := range []string{"createdAt", "updatedAt", "archivedAt"} {
		t.Run(field, func(t *testing.T) {
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "id", "name": "Example", field: "SECRET-ECHOED-IN-TIMESTAMP"}})
			}))
			_, err := client.Get(t.Context(), "id")
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "SECRET-ECHOED-IN-TIMESTAMP")
		})
	}
}
