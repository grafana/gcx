package alert_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// mergeResponseWithRenames is a convert API response in which Grafana renamed
// a receiver and a time interval.
const mergeResponseWithRenames = `{
	"status": "success",
	"rename_resources": {
		"receivers": {"webhook": "webhook_mimir-prod", "email": "email_mimir-prod"},
		"time_intervals": {"weekends": "weekends_mimir-prod"}
	},
	"stats": {
		"added_route": "mimir-prod",
		"added_receivers": ["email_mimir-prod", "webhook_mimir-prod"],
		"added_time_intervals": ["weekends_mimir-prod"],
		"added_templates": ["default.tmpl"]
	}
}`

// serveConvertAPI answers every request with status and body and records the
// last request.
func serveConvertAPI(t *testing.T, got *recordedRequest, status int, body string) fakeGrafanaConfigLoader {
	t.Helper()
	return newAlertFixture(t, recordRequests(t, got, status, body))
}

// rejectAPICalls fails the test when the command reaches the API.
func rejectAPICalls(t *testing.T) fakeGrafanaConfigLoader {
	t.Helper()
	return newAlertFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
}

func alertmanagerFile(t *testing.T) string {
	t.Helper()
	return writeFile(t, t.TempDir(), "alertmanager.yaml", plainAlertmanagerConfig)
}

func TestAlertmanagerImportUpsert_TextOutputGolden(t *testing.T) {
	tests := []struct {
		name   string
		flags  []string
		status int
		body   string
	}{
		{name: "alertmanager_import_staged", status: http.StatusAccepted, body: mergeResponseWithRenames},
		{name: "alertmanager_import_dry_run", flags: []string{"--dry-run"}, status: http.StatusOK, body: mergeResponseWithRenames},
		{name: "alertmanager_import_promoted", flags: []string{"--promote", "--force"}, status: http.StatusAccepted, body: mergeResponseWithRenames},
		{name: "alertmanager_import_staged_no_changes", status: http.StatusAccepted, body: `{"status":"success","stats":{}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, false)

			var got recordedRequest
			loader := serveConvertAPI(t, &got, tt.status, tt.body)
			args := append([]string{"alertmanager-imports", "upsert", "mimir-prod", "-f", alertmanagerFile(t)}, tt.flags...)
			stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader), args, "")

			require.NoError(t, err)
			assert.Empty(t, stderr)
			testutils.Golden(t, tt.name, stdout)
		})
	}
}

func TestAlertmanagerImportUpsert_SendsInputAndTemplates(t *testing.T) {
	setAgentMode(t, false)

	dir := t.TempDir()
	input := writeFile(t, dir, "alertmanager.yaml", plainAlertmanagerConfig)
	template := writeFile(t, dir, "default.tmpl", `{{ define "x" }}x{{ end }}`)
	var got recordedRequest
	loader := serveConvertAPI(t, &got, http.StatusAccepted, mergeResponseWithRenames)

	_, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
		[]string{"alertmanager-imports", "upsert", "-f", input, "--template", template, "--force-replace"}, "")

	require.NoError(t, err)
	assert.Equal(t, "imported", got.header.Get(identifierHeader), "the identifier defaults to imported")
	assert.Equal(t, "true", got.header.Get(forceReplaceHeader))
	assert.Empty(t, got.header.Get(dryRunHeader))
	assert.Empty(t, got.header.Get(promoteHeader))
	var body alert.AlertmanagerUserConfig
	require.NoError(t, json.Unmarshal(got.body, &body))
	assert.Equal(t, alert.AlertmanagerUserConfig{
		AlertmanagerConfig: plainAlertmanagerConfig,
		TemplateFiles:      map[string]string{"default.tmpl": `{{ define "x" }}x{{ end }}`},
	}, body)
}

func TestAlertmanagerImportUpsert_RejectsInvalidInputBeforeAPICall(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		stdin    string
		wantErr  string
		wantExit int
	}{
		{name: "missing filename", args: []string{}, wantErr: "--filename / -f is required", wantExit: gcxerrors.ExitUsageError},
		{name: "empty stdin", args: []string{"-f", "-"}, stdin: "\n", wantErr: "input is empty", wantExit: gcxerrors.ExitGeneralError},
		{name: "empty identifier", args: []string{" ", "-f", "-"}, stdin: "route: {}", wantErr: "IDENTIFIER must not be empty", wantExit: gcxerrors.ExitUsageError},
		{name: "two identifiers", args: []string{"a", "b", "-f", "-"}, stdin: "route: {}", wantErr: "accepts at most 1 arg(s)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, false)

			args := append([]string{"alertmanager-imports", "upsert"}, tt.args...)
			stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(rejectAPICalls(t)), args, tt.stdin)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			if tt.wantExit != 0 {
				// Cobra argument-count errors get exit code 2 at the root, not here.
				assert.Equal(t, tt.wantExit, exitCodeOf(err))
			}
			assert.Empty(t, stdout)
		})
	}
}

func TestAlertmanagerImportUpsert_AgentModeResult(t *testing.T) {
	tests := []struct {
		name       string
		flags      []string
		status     int
		wantAction string
		wantDryRun any
		wantPromo  bool
	}{
		{name: "staged", status: http.StatusAccepted, wantAction: "staged", wantDryRun: nil},
		{name: "dry run", flags: []string{"--dry-run"}, status: http.StatusOK, wantAction: "validated", wantDryRun: true},
		{name: "promoted", flags: []string{"--promote", "--force"}, status: http.StatusAccepted, wantAction: "promoted", wantPromo: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, true)

			var got recordedRequest
			loader := serveConvertAPI(t, &got, tt.status, mergeResponseWithRenames)
			args := append([]string{"alertmanager-imports", "upsert", "mimir-prod", "-f", alertmanagerFile(t)}, tt.flags...)
			stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader), args, "")

			require.NoError(t, err)
			assert.Equal(t, "mimir-prod", got.header.Get(identifierHeader), "the positional identifier is sent")
			doc, ok := decodeSingleJSONDocument(t, stdout).(map[string]any)
			require.True(t, ok, "result should be a JSON object, got: %q", stdout)
			assert.Equal(t, "gcx.mutation", doc["type"])
			assert.Equal(t, "1", doc["schema_version"])
			assert.Equal(t, tt.wantAction, doc["action"])
			assert.Equal(t, tt.wantDryRun, doc["dry_run"])
			assert.Equal(t, tt.wantPromo, doc["promoted"])
			assert.Equal(t, "mimir-prod", doc["identifier"])
			assert.Equal(t, map[string]any{"kind": "alertmanager-import", "name": "mimir-prod"}, doc["target"])
			assert.Equal(t, map[string]any{
				"added_route":            "mimir-prod",
				"added_receivers":        []any{"email_mimir-prod", "webhook_mimir-prod"},
				"added_time_intervals":   []any{"weekends_mimir-prod"},
				"added_templates":        []any{"default.tmpl"},
				"added_inhibition_rules": []any{},
			}, doc["stats"])
			assert.Equal(t, map[string]any{
				"receivers":      map[string]any{"webhook": "webhook_mimir-prod", "email": "email_mimir-prod"},
				"time_intervals": map[string]any{"weekends": "weekends_mimir-prod"},
			}, doc["rename_resources"])
		})
	}
}

func TestAlertmanagerImportUpsert_AgentModeEmptyResultKeepsShape(t *testing.T) {
	setAgentMode(t, true)

	var got recordedRequest
	loader := serveConvertAPI(t, &got, http.StatusAccepted, `{"status":"success"}`)
	stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
		[]string{"alertmanager-imports", "upsert", "-f", alertmanagerFile(t)}, "")

	require.NoError(t, err)
	doc, ok := decodeSingleJSONDocument(t, stdout).(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{
		"added_route":            "",
		"added_receivers":        []any{},
		"added_time_intervals":   []any{},
		"added_templates":        []any{},
		"added_inhibition_rules": []any{},
	}, doc["stats"])
	assert.Equal(t, map[string]any{"receivers": map[string]any{}, "time_intervals": map[string]any{}}, doc["rename_resources"])
}

func TestAlertmanagerImportUpsert_PromoteAsksForConfirmation(t *testing.T) {
	t.Run("agent mode without --force is rejected", func(t *testing.T) {
		setAgentMode(t, true)

		stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(rejectAPICalls(t)),
			[]string{"alertmanager-imports", "upsert", "mimir-prod", "-f", alertmanagerFile(t), "--promote"}, "")

		require.ErrorIs(t, err, providers.ErrAgentModeRequiresForce)
		assert.Empty(t, stdout)
	})

	t.Run("stdin without --force is rejected", func(t *testing.T) {
		setAgentMode(t, false)

		stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(rejectAPICalls(t)),
			[]string{"alertmanager-imports", "upsert", "mimir-prod", "-f", "-", "--promote"}, plainAlertmanagerConfig)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "use --force")
		assert.Empty(t, stdout)
	})

	t.Run("declined prompt exits as cancelled", func(t *testing.T) {
		setAgentMode(t, false)

		stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(rejectAPICalls(t)),
			[]string{"alertmanager-imports", "upsert", "mimir-prod", "-f", alertmanagerFile(t), "--promote"}, "n\n")

		require.Error(t, err)
		assert.Equal(t, gcxerrors.ExitCancelled, exitCodeOf(err))
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, `Promote Alertmanager configuration "mimir-prod" into the Grafana configuration?`)
		assert.Contains(t, stderr, "Aborted.")
	})

	t.Run("accepted prompt promotes", func(t *testing.T) {
		setAgentMode(t, false)

		var got recordedRequest
		loader := serveConvertAPI(t, &got, http.StatusAccepted, mergeResponseWithRenames)
		stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
			[]string{"alertmanager-imports", "upsert", "mimir-prod", "-f", alertmanagerFile(t), "--promote"}, "y\n")

		require.NoError(t, err)
		assert.Contains(t, stderr, "[y/N]")
		assert.Equal(t, "true", got.header.Get(promoteHeader))
		testutils.Golden(t, "alertmanager_import_promoted", stdout)
	})

	for _, agentMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry run does not prompt (agent mode %t)", agentMode), func(t *testing.T) {
			setAgentMode(t, agentMode)

			var got recordedRequest
			loader := serveConvertAPI(t, &got, http.StatusOK, mergeResponseWithRenames)
			// No stdin and no --force: a prompt would read EOF and cancel.
			stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
				[]string{"alertmanager-imports", "upsert", "mimir-prod", "-f", alertmanagerFile(t), "--promote", "--dry-run"}, "")

			require.NoError(t, err)
			assert.Empty(t, stderr)
			assert.NotEmpty(t, stdout)
			assert.Equal(t, "true", got.header.Get(promoteHeader))
			assert.Equal(t, "true", got.header.Get(dryRunHeader))
		})
	}
}

// TestAlertmanagerImport_AgentModeErrors checks that a failed import writes
// nothing to stdout and that the in-band error document carries the remedy.
func TestAlertmanagerImport_AgentModeErrors(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantSummary string
		wantDetails string
	}{
		{
			name:        "feature toggle off",
			status:      http.StatusNotImplemented,
			body:        `{"message": "Not Implemented"}`,
			wantSummary: gcxerrors.SummaryEndpointNotAvailable,
			wantDetails: "alertingImportAlertmanagerAPI",
		},
		{
			name:   "external alertmanager sync is configured",
			status: http.StatusConflict,
			body: `{"message": "alertmanager configuration import is disabled while external alertmanager ` +
				`sync is configured for this organization"}`,
			wantSummary: gcxerrors.SummaryResourceConflict,
			wantDetails: "External Alertmanager sync is configured for this organization",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, true)

			var got recordedRequest
			loader := serveConvertAPI(t, &got, tt.status, tt.body)
			stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
				[]string{"alertmanager-imports", "upsert", "-f", alertmanagerFile(t)}, "")

			require.Error(t, err)
			assert.Empty(t, stdout, "the error document is written by the root error reporter, not the command")

			detailed := fail.ErrorToDetailedError(err)
			require.NotNil(t, detailed)
			var envelope bytes.Buffer
			require.NoError(t, detailed.WriteJSON(&envelope, exitCodeOf(err)))
			doc, ok := decodeSingleJSONDocument(t, envelope.String()).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "gcx.error", doc["type"])
			errorObject, ok := doc["error"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.wantSummary, errorObject["summary"])
			assert.Contains(t, errorObject["details"], tt.wantDetails)
			assert.InDelta(t, gcxerrors.ExitGeneralError, errorObject["exitCode"], 0)
		})
	}
}

func TestAlertmanagerImportGet(t *testing.T) {
	const response = `{"alertmanager_config": "route:\n  receiver: webhook\n", "template_files": {"a.tmpl": "a"}}`
	want := alert.AlertmanagerUserConfig{
		AlertmanagerConfig: "route:\n  receiver: webhook\n",
		TemplateFiles:      map[string]string{"a.tmpl": "a"},
	}

	t.Run("explicit YAML keeps the user config fields", func(t *testing.T) {
		setAgentMode(t, false)

		var got recordedRequest
		loader := serveConvertAPI(t, &got, http.StatusOK, response)
		stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
			[]string{"alertmanager-imports", "get", "-o", "yaml"}, "")

		require.NoError(t, err)
		assert.Equal(t, "imported", got.header.Get(identifierHeader), "the identifier defaults to imported")
		var decoded alert.AlertmanagerUserConfig
		require.NoError(t, yaml.UnmarshalStrict([]byte(stdout), &decoded))
		assert.Equal(t, want, decoded)
	})

	t.Run("explicit JSON keeps the user config fields", func(t *testing.T) {
		setAgentMode(t, false)

		var got recordedRequest
		loader := serveConvertAPI(t, &got, http.StatusOK, response)
		stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
			[]string{"alertmanager-imports", "get", "-o", "json"}, "")

		require.NoError(t, err)
		var decoded alert.AlertmanagerUserConfig
		require.NoError(t, json.Unmarshal([]byte(stdout), &decoded))
		assert.Equal(t, want, decoded)
	})

	t.Run("agent mode emits one JSON document", func(t *testing.T) {
		setAgentMode(t, true)

		var got recordedRequest
		loader := serveConvertAPI(t, &got, http.StatusOK, response)
		stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
			[]string{"alertmanager-imports", "get", "mimir-prod"}, "")

		require.NoError(t, err)
		assert.Equal(t, "mimir-prod", got.header.Get(identifierHeader))
		doc := decodeSingleJSONDocument(t, stdout)
		assert.Equal(t, map[string]any{
			"alertmanager_config": "route:\n  receiver: webhook\n",
			"template_files":      map[string]any{"a.tmpl": "a"},
		}, doc)
	})
}

func TestAlertmanagerImportGetTableGolden(t *testing.T) {
	const config = `route:
  receiver: default-email
  group_by: [alertname, cluster]
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h
  routes:
    - receiver: pagerduty-payments
      matchers:
        - env="prod"
      match:
        team: payments
      mute_time_intervals: [weekends]
    - receiver: slack-alerts
      matchers:
        - severity=~"warning|info"
      group_by: [alertname]
      continue: true
      routes:
        - receiver: "null"
          matchers:
            - env="staging"
          routes:
            - receiver: default-email
              match_re:
                instance: ".+"
    - receiver: "null"
      match:
        alertname: Watchdog
      active_time_intervals: [business-hours]
receivers:
  - name: default-email
    email_configs:
      - to: alerts@example.com
  - name: pagerduty-payments
    pagerduty_configs:
      - routing_key: <secret>
  - name: slack-alerts
    slack_configs:
      - api_url: <secret>
      - api_url: <secret>
  - name: "null"
mute_time_intervals:
  - name: weekends
    time_intervals:
      - weekdays: [saturday, sunday]
time_intervals:
  - name: business-hours
    time_intervals:
      - weekdays: [monday:friday]
        times:
          - start_time: "09:00"
            end_time: "17:00"
        location: Europe/Berlin
inhibit_rules:
  - source_matchers:
      - region="eu"
    source_match:
      severity: critical
    source_match_re:
      service: api|web
    target_matchers:
      - severity="warning"
    equal: [alertname, cluster]
`
	templates := map[string]string{
		"email.tmpl": `{{ define "email.subject" }}subject{{ end }}
{{- define "email.body" -}}body{{ end }}
{{ define "email.with.a.very.long.descriptive.name" }}long{{ end }}` + strings.Repeat("x", 1050),
		"slack.tmpl": `{{ define "slack.title" }}title{{ end }}` + strings.Repeat("x", 272),
	}

	tests := []struct {
		name      string
		config    string
		templates map[string]string
		args      []string
	}{
		{name: "alertmanager_import_get_table", config: config, templates: templates},
		{
			name:      "alertmanager_import_get_table_wide",
			config:    config,
			templates: templates,
			args:      []string{"-o", "wide"},
		},
		{
			name: "alertmanager_import_get_table_empty_sections",
			config: `route:
  receiver: default-email
receivers:
  - name: default-email
    email_configs:
      - to: alerts@example.com
`,
			templates: map[string]string{},
		},
		{
			name:      "alertmanager_import_get_table_invalid",
			config:    "route: [",
			templates: map[string]string{"fallback.tmpl": `{{ define "fallback" }}value{{ end }}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, false)
			terminal.ResetForTesting()
			t.Cleanup(terminal.ResetForTesting)

			body, err := json.Marshal(alert.AlertmanagerUserConfig{
				AlertmanagerConfig: tt.config,
				TemplateFiles:      tt.templates,
			})
			require.NoError(t, err)

			var got recordedRequest
			loader := serveConvertAPI(t, &got, http.StatusOK, string(body))
			args := append([]string{"alertmanager-imports", "get", "mimir-prod"}, tt.args...)
			stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader), args, "")

			require.NoError(t, err)
			assert.Empty(t, stderr)
			assert.Equal(t, "mimir-prod", got.header.Get(identifierHeader))
			testutils.Golden(t, tt.name, stdout)
		})
	}
}

func TestAlertmanagerImportGetTableHonorsNoTruncate(t *testing.T) {
	setAgentMode(t, false)
	terminal.ResetForTesting()
	terminal.SetNoTruncate(true)
	t.Cleanup(terminal.ResetForTesting)

	const (
		defineName  = "template.with.a.name.longer.than.the.default.table.cell.width"
		longMatcher = `label="abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuv"`
	)
	body, err := json.Marshal(alert.AlertmanagerUserConfig{
		AlertmanagerConfig: `route:
  receiver: default
  routes:
    - receiver: child
      matchers:
        - ` + longMatcher + "\n",
		TemplateFiles: map[string]string{
			"long.tmpl": `{{ define "` + defineName + `" }}value{{ end }}`,
		},
	})
	require.NoError(t, err)

	var got recordedRequest
	loader := serveConvertAPI(t, &got, http.StatusOK, string(body))
	stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
		[]string{"alertmanager-imports", "get"}, "")

	require.NoError(t, err)
	assert.Contains(t, stdout, defineName)
	assert.Contains(t, stdout, "└─ "+longMatcher)
	assert.NotContains(t, stdout, "template.with.a.name.longer.than.the.de...")
}

func TestAlertmanagerImportGetTableTruncatesOnlyMatcherTextAfterPrefix(t *testing.T) {
	setAgentMode(t, false)
	terminal.ResetForTesting()
	t.Cleanup(terminal.ResetForTesting)

	const longMatcher = `label="abcdefghijklmnopqrstuvwxyzéééééééééééééééééééé"`
	body, err := json.Marshal(alert.AlertmanagerUserConfig{
		AlertmanagerConfig: `route:
  receiver: default
  routes:
    - receiver: child
      routes:
        - receiver: grandchild
          matchers:
            - ` + longMatcher + `
    - receiver: unmatched
`,
		TemplateFiles: map[string]string{},
	})
	require.NoError(t, err)

	var got recordedRequest
	loader := serveConvertAPI(t, &got, http.StatusOK, string(body))
	stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
		[]string{"alertmanager-imports", "get"}, "")

	require.NoError(t, err)
	assert.Contains(t, stdout, "├─ -")
	assert.Contains(t, stdout, "│  └─ "+string([]rune(longMatcher)[:37])+"...")
	assert.NotContains(t, stdout, "│  └─ "+longMatcher)
	assert.NotContains(t, stdout, "ROUTE")
}

func TestAlertmanagerImportGetTableHandlesSparseConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   []string
	}{
		{name: "absent route", config: `{}`, want: []string{"Routes (0):"}},
		{
			name: "empty receivers",
			config: `route:
  receiver: default
receivers: []
`,
			want: []string{"Receivers (0):"},
		},
		{
			name: "object integration config",
			config: `route:
  receiver: object
receivers:
  - name: object
    email_configs: {}
`,
			want: []string{"object  none"},
		},
		{
			name: "null integration config",
			config: `route:
  receiver: null-config
receivers:
  - name: null-config
    email_configs: null
`,
			want: []string{"null-config  none"},
		},
		{
			name: "group by all labels",
			config: `route:
  receiver: default
  routes:
    - receiver: child
      group_by: ['...']
`,
			want: []string{"child", "..."},
		},
		{
			name: "empty child group by inherits",
			config: `route:
  receiver: default
  group_by: [alertname]
  routes:
    - receiver: child
      group_by: []
`,
			want: []string{"child", "(inherited)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, false)
			terminal.ResetForTesting()
			t.Cleanup(terminal.ResetForTesting)

			body, err := json.Marshal(alert.AlertmanagerUserConfig{
				AlertmanagerConfig: tt.config,
				TemplateFiles:      map[string]string{},
			})
			require.NoError(t, err)

			var got recordedRequest
			loader := serveConvertAPI(t, &got, http.StatusOK, string(body))
			stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
				[]string{"alertmanager-imports", "get"}, "")

			require.NoError(t, err)
			assert.Empty(t, stderr)
			for _, want := range tt.want {
				assert.Contains(t, stdout, want)
			}
		})
	}
}

func TestAlertmanagerImportGetTableKeepsDuplicateIntervalsAndSortsIntegrations(t *testing.T) {
	setAgentMode(t, false)
	terminal.ResetForTesting()
	t.Cleanup(terminal.ResetForTesting)

	body, err := json.Marshal(alert.AlertmanagerUserConfig{
		AlertmanagerConfig: `route:
  receiver: mixed
receivers:
  - name: mixed
    webhook_configs:
      - url: https://example.com
    email_configs:
      - to: alerts@example.com
mute_time_intervals:
  - name: duplicate
    time_intervals: []
time_intervals:
  - name: duplicate
    time_intervals: []
`,
		TemplateFiles: map[string]string{},
	})
	require.NoError(t, err)

	var got recordedRequest
	loader := serveConvertAPI(t, &got, http.StatusOK, string(body))
	stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
		[]string{"alertmanager-imports", "get"}, "")

	require.NoError(t, err)
	assert.Contains(t, stdout, "mixed  email, webhook")
	assert.Equal(t, 2, strings.Count(stdout, "duplicate  0"))
}

func TestAlertmanagerImportCommands_RejectEmptyIdentifier(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "get", args: []string{"alertmanager-imports", "get", ""}},
		{name: "promote", args: []string{"alertmanager-imports", "promote", "", "--force"}},
		{name: "delete", args: []string{"alertmanager-imports", "delete", "", "--force"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, false)

			stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(rejectAPICalls(t)), tt.args, "")

			require.EqualError(t, err, "IDENTIFIER must not be empty")
			assert.Equal(t, gcxerrors.ExitUsageError, exitCodeOf(err))
			assert.Empty(t, stdout)
		})
	}
}

func TestAlertmanagerImportPromoteAndDelete(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
		wantAction string
	}{
		{
			name:       "promote",
			args:       []string{"alertmanager-imports", "promote", "mimir-prod"},
			wantMethod: http.MethodPost,
			wantPath:   "/api/convert/api/v1/alerts/mimir-prod/promote",
			wantAction: "promoted",
		},
		{
			name:       "delete",
			args:       []string{"alertmanager-imports", "delete", "mimir-prod"},
			wantMethod: http.MethodDelete,
			wantPath:   "/api/convert/api/v1/alerts",
			wantAction: "deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" with --force in agent mode", func(t *testing.T) {
			setAgentMode(t, true)

			var got recordedRequest
			loader := serveConvertAPI(t, &got, http.StatusAccepted, mergeResponseWithRenames)
			stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader), append(tt.args, "--force"), "")

			require.NoError(t, err)
			assert.Equal(t, tt.wantMethod, got.method)
			assert.Equal(t, tt.wantPath, got.path)
			doc, ok := decodeSingleJSONDocument(t, stdout).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "gcx.mutation", doc["type"])
			assert.Equal(t, tt.wantAction, doc["action"])
			assert.Equal(t, map[string]any{"kind": "alertmanager-import", "name": "mimir-prod"}, doc["target"])
		})

		t.Run(tt.name+" without --force in agent mode is rejected", func(t *testing.T) {
			setAgentMode(t, true)

			stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(rejectAPICalls(t)), tt.args, "")

			require.ErrorIs(t, err, providers.ErrAgentModeRequiresForce)
			assert.Empty(t, stdout)
		})

		t.Run(tt.name+" declined prompt exits as cancelled", func(t *testing.T) {
			setAgentMode(t, false)

			stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(rejectAPICalls(t)), tt.args, "n\n")

			require.Error(t, err)
			assert.Equal(t, gcxerrors.ExitCancelled, exitCodeOf(err))
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, `"mimir-prod"`)
			assert.Contains(t, stderr, "[y/N]")
			assert.Contains(t, stderr, "Aborted.")
		})
	}
}

func TestAlertmanagerImportPromoteAndDelete_HumanOutput(t *testing.T) {
	t.Run("delete prints one success line", func(t *testing.T) {
		setAgentMode(t, false)

		var got recordedRequest
		loader := serveConvertAPI(t, &got, http.StatusAccepted, `{"status":"success"}`)
		stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
			[]string{"alertmanager-imports", "delete", "mimir-prod"}, "y\n")

		require.NoError(t, err)
		assert.Contains(t, stderr, `Delete staged Alertmanager configuration "mimir-prod"? [y/N]`)
		assert.Equal(t, "✔ Deleted staged Alertmanager configuration \"mimir-prod\"\n", stdout)
	})

	t.Run("promote prints the merge result", func(t *testing.T) {
		setAgentMode(t, false)

		var got recordedRequest
		loader := serveConvertAPI(t, &got, http.StatusAccepted, mergeResponseWithRenames)
		stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
			[]string{"alertmanager-imports", "promote", "mimir-prod", "--force"}, "")

		require.NoError(t, err)
		testutils.Golden(t, "alertmanager_import_promoted", stdout)
	})
}
