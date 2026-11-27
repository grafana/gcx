package alert_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	identifierHeader   = "X-Grafana-Alerting-Config-Identifier"
	dryRunHeader       = "X-Grafana-Alerting-Dry-Run"
	promoteHeader      = "X-Grafana-Alerting-Promote"
	forceReplaceHeader = "X-Grafana-Alerting-Config-Force-Replace"
)

// recordedRequest is what the fake convert API saw.
type recordedRequest struct {
	method string
	path   string
	header http.Header
	body   []byte
}

// recordRequests answers every request with status and body, and records the
// last request.
func recordRequests(t *testing.T, got *recordedRequest, status int, body string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		*got = recordedRequest{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: data}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestClient_ImportAlertmanagerConfig_Request(t *testing.T) {
	userConfig := alert.AlertmanagerUserConfig{
		AlertmanagerConfig: plainAlertmanagerConfig,
		TemplateFiles:      map[string]string{"default.tmpl": `{{ define "x" }}<b>&</b>{{ end }}`},
	}

	tests := []struct {
		name        string
		opts        alert.AlertmanagerImportOptions
		wantHeaders map[string]string
	}{
		{
			name:        "stage sends only the identifier",
			opts:        alert.AlertmanagerImportOptions{Identifier: "imported"},
			wantHeaders: map[string]string{identifierHeader: "imported"},
		},
		{
			name:        "dry run",
			opts:        alert.AlertmanagerImportOptions{Identifier: "mimir-prod", DryRun: true},
			wantHeaders: map[string]string{identifierHeader: "mimir-prod", dryRunHeader: "true"},
		},
		{
			name:        "promote",
			opts:        alert.AlertmanagerImportOptions{Identifier: "mimir-prod", Promote: true},
			wantHeaders: map[string]string{identifierHeader: "mimir-prod", promoteHeader: "true"},
		},
		{
			name: "force replace",
			opts: alert.AlertmanagerImportOptions{Identifier: "mimir-prod", ForceReplace: true},
			wantHeaders: map[string]string{
				identifierHeader:   "mimir-prod",
				forceReplaceHeader: "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got recordedRequest
			server := httptest.NewServer(recordRequests(t, &got, http.StatusAccepted, `{"status":"success"}`))
			t.Cleanup(server.Close)

			_, err := newTestClient(t, server).ImportAlertmanagerConfig(t.Context(), userConfig, tt.opts)

			require.NoError(t, err)
			assert.Equal(t, http.MethodPost, got.method)
			assert.Equal(t, "/api/convert/api/v1/alerts", got.path)
			assert.Equal(t, "application/json", got.header.Get("Content-Type"))
			for _, name := range []string{identifierHeader, dryRunHeader, promoteHeader, forceReplaceHeader} {
				want, set := tt.wantHeaders[name]
				assert.Equal(t, set, got.header.Get(name) != "", "header %s present", name)
				assert.Equal(t, want, got.header.Get(name), "header %s", name)
			}

			var body map[string]any
			require.NoError(t, json.Unmarshal(got.body, &body))
			assert.Equal(t, map[string]any{
				"alertmanager_config": plainAlertmanagerConfig,
				"template_files":      map[string]any{"default.tmpl": `{{ define "x" }}<b>&</b>{{ end }}`},
			}, body, "the configuration string and the templates arrive unchanged")
		})
	}
}

func TestClient_ImportAlertmanagerConfig_DecodesMergeResult(t *testing.T) {
	var got recordedRequest
	server := httptest.NewServer(recordRequests(t, &got, http.StatusOK, `{
		"status": "success",
		"rename_resources": {"receivers": {"default": "default_mimir-prod"}},
		"stats": {"added_route": "mimir-prod", "added_receivers": ["default_mimir-prod"]}
	}`))
	t.Cleanup(server.Close)

	merge, err := newTestClient(t, server).ImportAlertmanagerConfig(t.Context(),
		alert.AlertmanagerUserConfig{AlertmanagerConfig: "route: {}"},
		alert.AlertmanagerImportOptions{Identifier: "mimir-prod", DryRun: true})

	require.NoError(t, err)
	assert.Equal(t, &alert.AlertmanagerMergeResult{
		RenameResources: &alert.AlertmanagerRenames{Receivers: map[string]string{"default": "default_mimir-prod"}},
		Stats:           &alert.AlertmanagerMergeStats{AddedRoute: "mimir-prod", AddedReceivers: []string{"default_mimir-prod"}},
	}, merge)
}

func TestClient_GetImportedAlertmanagerConfig(t *testing.T) {
	var got recordedRequest
	server := httptest.NewServer(recordRequests(t, &got, http.StatusOK,
		`{"alertmanager_config": "route:\n  receiver: webhook\n", "template_files": {"a.tmpl": "a"}}`))
	t.Cleanup(server.Close)

	userConfig, err := newTestClient(t, server).GetImportedAlertmanagerConfig(t.Context(), "mimir-prod")

	require.NoError(t, err)
	assert.Equal(t, http.MethodGet, got.method)
	assert.Equal(t, "/api/convert/api/v1/alerts", got.path)
	assert.Equal(t, "mimir-prod", got.header.Get(identifierHeader))
	assert.Equal(t, "application/json", got.header.Get("Accept"), "the API answers in YAML unless JSON is requested")
	assert.Equal(t, &alert.AlertmanagerUserConfig{
		AlertmanagerConfig: "route:\n  receiver: webhook\n",
		TemplateFiles:      map[string]string{"a.tmpl": "a"},
	}, userConfig)
}

func TestClient_DeleteImportedAlertmanagerConfig(t *testing.T) {
	var got recordedRequest
	server := httptest.NewServer(recordRequests(t, &got, http.StatusAccepted, `{"status":"success"}`))
	t.Cleanup(server.Close)

	err := newTestClient(t, server).DeleteImportedAlertmanagerConfig(t.Context(), "mimir-prod")

	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, got.method)
	assert.Equal(t, "/api/convert/api/v1/alerts", got.path)
	assert.Equal(t, "mimir-prod", got.header.Get(identifierHeader))
}

func TestClient_PromoteImportedAlertmanagerConfig(t *testing.T) {
	var got recordedRequest
	server := httptest.NewServer(recordRequests(t, &got, http.StatusAccepted,
		`{"status": "success", "stats": {"added_route": "mimir-prod"}}`))
	t.Cleanup(server.Close)

	merge, err := newTestClient(t, server).PromoteImportedAlertmanagerConfig(t.Context(), "mimir-prod")

	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, got.method)
	assert.Equal(t, "/api/convert/api/v1/alerts/mimir-prod/promote", got.path)
	assert.Empty(t, got.body, "promote sends no body")
	assert.Empty(t, got.header.Get(dryRunHeader), "promote has no dry run")
	assert.Equal(t, "mimir-prod", merge.Stats.AddedRoute)
}

func TestClient_AlertmanagerImportErrors(t *testing.T) {
	tests := []struct {
		name            string
		status          int
		body            string
		wantSummary     string
		wantDetails     string
		wantSuggestions []string
	}{
		{
			name:        "feature toggle off",
			status:      http.StatusNotImplemented,
			body:        `{"message": "Not Implemented"}`,
			wantSummary: gcxerrors.SummaryEndpointNotAvailable,
			wantDetails: "Alertmanager import API is not enabled on this Grafana instance (feature toggle alertingImportAlertmanagerAPI)",
		},
		{
			name:   "external alertmanager sync is configured",
			status: http.StatusConflict,
			body: `{"message": "alertmanager configuration import is disabled while external alertmanager ` +
				`sync is configured for this organization"}`,
			wantSummary: gcxerrors.SummaryResourceConflict,
			wantDetails: "External Alertmanager sync is configured for this organization. " +
				"Disable the sync before you import an Alertmanager configuration manually.",
		},
		{
			name:   "another import is staged",
			status: http.StatusConflict,
			body: `{"message": "multiple extra configurations are not supported, found another configuration ` +
				`with identifier: other", "messageId": "alerting.notifications.alertmanager.multipleExtraConfigsUnsupported"}`,
			wantSummary: gcxerrors.SummaryResourceConflict,
			wantDetails: "Grafana keeps one imported Alertmanager configuration per organization: multiple extra " +
				"configurations are not supported, found another configuration with identifier: other",
			wantSuggestions: []string{
				"Delete the staged import: gcx alert alertmanager-imports delete other",
				"Promote the staged import: gcx alert alertmanager-imports promote other",
				"Replace the staged import: add --force-replace",
			},
		},
		{
			name:        "another import is staged, without the identifier in the message",
			status:      http.StatusConflict,
			body:        `{"message": "multiple extra configurations are not supported"}`,
			wantSummary: gcxerrors.SummaryResourceConflict,
			wantDetails: "Grafana keeps one imported Alertmanager configuration per organization",
			wantSuggestions: []string{
				"Find the identifier of the staged import in the server message, then delete it: " +
					"gcx alert alertmanager-imports delete <identifier>",
				"Replace the staged import: add --force-replace",
			},
		},
		{
			name:        "another import is staged, with an invalid identifier in the message",
			status:      http.StatusConflict,
			body:        `{"message": "multiple extra configurations are not supported, found another configuration with identifier: other/config"}`,
			wantSummary: gcxerrors.SummaryResourceConflict,
			wantDetails: "Grafana keeps one imported Alertmanager configuration per organization",
			wantSuggestions: []string{
				"Find the identifier of the staged import in the server message, then delete it: " +
					"gcx alert alertmanager-imports delete <identifier>",
				"Replace the staged import: add --force-replace",
			},
		},
		{
			name:        "other conflicts keep the server message",
			status:      http.StatusConflict,
			body:        `{"message": "extra configuration with identifier x is managed by external alertmanager sync"}`,
			wantSummary: gcxerrors.SummaryResourceConflict,
			wantDetails: "extra configuration with identifier x is managed by external alertmanager sync (HTTP 409)",
		},
		{
			name:        "validation errors keep the server message",
			status:      http.StatusBadRequest,
			body:        `{"message": "unknown field typo_field"}`,
			wantSummary: gcxerrors.SummaryAPIError,
			wantDetails: "unknown field typo_field (HTTP 400)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got recordedRequest
			server := httptest.NewServer(recordRequests(t, &got, tt.status, tt.body))
			t.Cleanup(server.Close)

			_, err := newTestClient(t, server).ImportAlertmanagerConfig(t.Context(),
				alert.AlertmanagerUserConfig{AlertmanagerConfig: "route: {}"},
				alert.AlertmanagerImportOptions{Identifier: "imported"})

			require.Error(t, err)
			detailed := fail.ErrorToDetailedError(err)
			require.NotNil(t, detailed)
			assert.Equal(t, tt.wantSummary, detailed.Summary)
			assert.Contains(t, detailed.Details, tt.wantDetails)
			if tt.wantSuggestions != nil {
				assert.Equal(t, tt.wantSuggestions, detailed.Suggestions)
			}
			_, isStatusErr := errors.AsType[*gcxerrors.HTTPStatusError](err)
			assert.True(t, isStatusErr, "the HTTP status stays in the error chain")
		})
	}
}

// alertmanagerImportCall calls one convert API endpoint.
type alertmanagerImportCall struct {
	name string
	call func(ctx context.Context, client *alert.Client) error
}

// alertmanagerImportCalls returns one call for each convert API endpoint.
func alertmanagerImportCalls() []alertmanagerImportCall {
	return []alertmanagerImportCall{
		{name: "upsert", call: func(ctx context.Context, client *alert.Client) error {
			_, err := client.ImportAlertmanagerConfig(ctx, alert.AlertmanagerUserConfig{AlertmanagerConfig: "route: {}"},
				alert.AlertmanagerImportOptions{Identifier: "imported"})
			return err
		}},
		{name: "get", call: func(ctx context.Context, client *alert.Client) error {
			_, err := client.GetImportedAlertmanagerConfig(ctx, "missing")
			return err
		}},
		{name: "promote", call: func(ctx context.Context, client *alert.Client) error {
			_, err := client.PromoteImportedAlertmanagerConfig(ctx, "missing")
			return err
		}},
		{name: "delete", call: func(ctx context.Context, client *alert.Client) error {
			return client.DeleteImportedAlertmanagerConfig(ctx, "missing")
		}},
	}
}

func TestClient_AlertmanagerImportToggleOffOnEveryEndpoint(t *testing.T) {
	for _, tt := range alertmanagerImportCalls() {
		t.Run(tt.name, func(t *testing.T) {
			var got recordedRequest
			server := httptest.NewServer(recordRequests(t, &got, http.StatusNotImplemented, `{"message": "Not Implemented"}`))
			t.Cleanup(server.Close)

			err := tt.call(t.Context(), newTestClient(t, server))

			require.Error(t, err)
			detailed := fail.ErrorToDetailedError(err)
			require.NotNil(t, detailed)
			assert.Equal(t, gcxerrors.SummaryEndpointNotAvailable, detailed.Summary)
			assert.Contains(t, detailed.Details, "feature toggle alertingImportAlertmanagerAPI")
			_, isStatusErr := errors.AsType[*gcxerrors.HTTPStatusError](err)
			assert.True(t, isStatusErr, "the HTTP status stays in the error chain")
		})
	}
}

// TestClient_AlertmanagerImportNotFoundMeansMissingAPI checks that a 404 from
// upsert or delete is not reported as a missing import. Neither endpoint
// reports a missing import (Grafana deletes idempotently), so only a Grafana
// without the API answers 404.
func TestClient_AlertmanagerImportNotFoundMeansMissingAPI(t *testing.T) {
	for _, tt := range alertmanagerImportCalls() {
		if tt.name != "upsert" && tt.name != "delete" {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			var got recordedRequest
			server := httptest.NewServer(recordRequests(t, &got, http.StatusNotFound, `{"message": "Not found"}`))
			t.Cleanup(server.Close)

			err := tt.call(t.Context(), newTestClient(t, server))

			require.Error(t, err)
			require.NotErrorIs(t, err, alert.ErrAlertmanagerImportNotFound)
			detailed := fail.ErrorToDetailedError(err)
			require.NotNil(t, detailed)
			assert.Equal(t, gcxerrors.SummaryEndpointNotAvailable, detailed.Summary)
			assert.Equal(t, "The Alertmanager import API is not available on this Grafana instance (HTTP 404). "+
				"The Grafana version may predate the API.", detailed.Details)
		})
	}
}

func TestClient_AlertmanagerImportNotFound(t *testing.T) {
	notFound := `{"message": "Extra configuration not found", "messageId": "alerting.notifications.alertmanager.extraConfigNotFound"}`

	for _, tt := range alertmanagerImportCalls() {
		if tt.name != "get" && tt.name != "promote" {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			var got recordedRequest
			server := httptest.NewServer(recordRequests(t, &got, http.StatusNotFound, notFound))
			t.Cleanup(server.Close)

			err := tt.call(t.Context(), newTestClient(t, server))

			require.ErrorIs(t, err, alert.ErrAlertmanagerImportNotFound)
			detailed := fail.ErrorToDetailedError(err)
			require.NotNil(t, detailed)
			assert.Equal(t, gcxerrors.SummaryResourceNotFound, detailed.Summary)
			assert.Equal(t, `No imported Alertmanager configuration has the identifier "missing".`, detailed.Details)
			assert.Equal(t, []string{
				"Import a configuration: gcx alert alertmanager-imports upsert missing -f <file>",
				"If the identifier is correct, the Grafana version may predate the Alertmanager import API",
			}, detailed.Suggestions)
		})
	}
}
