package alert_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/config"
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

type datasourceImportFixture struct {
	datasourceStatus int
	datasourceBody   string
	proxyStatus      int
	proxyBody        string
	convertStatus    int
	convertBody      string
	convertRequest   recordedRequest
	paths            []string
}

func serveDatasourceImportAPI(t *testing.T, fixture *datasourceImportFixture) fakeGrafanaConfigLoader {
	t.Helper()
	return newAlertFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fixture.paths = append(fixture.paths, r.URL.EscapedPath())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/datasources/uid/"):
			assert.Equal(t, http.MethodGet, r.Method)
			status := fixture.datasourceStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(fixture.datasourceBody))
		case strings.HasPrefix(r.URL.Path, "/api/datasources/proxy/uid/"):
			assert.Equal(t, http.MethodGet, r.Method)
			status := fixture.proxyStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(fixture.proxyBody))
		case r.URL.Path == "/api/convert/api/v1/alerts":
			assert.Equal(t, http.MethodPost, r.Method)
			data, err := io.ReadAll(r.Body)
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			fixture.convertRequest = recordedRequest{
				method: r.Method,
				path:   r.URL.Path,
				header: r.Header.Clone(),
				body:   data,
			}
			status := fixture.convertStatus
			if status == 0 {
				status = http.StatusAccepted
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(fixture.convertBody))
		default:
			http.NotFound(w, r)
		}
	})
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

func TestAlertmanagerImportUpsert_KeepsTemplateNames(t *testing.T) {
	setAgentMode(t, false)

	for _, tt := range []struct {
		name      string
		input     string
		templates string
	}{
		{
			name: "YAML with extra wrapper keys",
			input: `alertmanager_config: 'route: {}'
metadata: {source: mimir}
route: {receiver: ignored}
template_files: {yes: first, on: second, 1.0: third, x.tmpl: fourth}
`,
			templates: `{"yes": "first", "on": "second", "1.0": "third", "x.tmpl": "fourth"}`,
		},
		{
			name:      "JSON",
			input:     `{"alertmanager_config": "route: {}", "template_files": {"yes": "first", "on": "second", "1.0": "third", "x.tmpl": "fourth"}}`,
			templates: `{"yes": "first", "on": "second", "1.0": "third", "x.tmpl": "fourth"}`,
		},
		{
			name:      "large numeric name",
			input:     "alertmanager_config: 'route: {}'\ntemplate_files: {9223372036854775808: first}\n",
			templates: `{"9223372036854775808": "first"}`,
		},
		{
			name:      "unhandled tag on a sibling",
			input:     "alertmanager_config: 'route: {}'\ntemplate_files: {!local x: first, yes: second}\n",
			templates: `{"x": "first", "yes": "second"}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got recordedRequest
			loader := serveConvertAPI(t, &got, http.StatusAccepted, mergeResponseWithRenames)
			_, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
				[]string{"alertmanager-imports", "upsert", "-f", "-"}, tt.input)

			require.NoError(t, err)
			assert.JSONEq(t, `{"alertmanager_config": "route: {}", "template_files": `+tt.templates+`}`, string(got.body))
		})
	}
}

func TestAlertmanagerImportUpsert_RejectsOversizedInputBeforeAPICall(t *testing.T) {
	setAgentMode(t, false)

	// Invalid YAML, exactly one byte over 10 MiB. The size error must come first.
	input := "[" + strings.Repeat(" ", 10<<20)
	for _, source := range []string{"stdin", "file"} {
		t.Run(source, func(t *testing.T) {
			file, stdin := "-", input
			if source == "file" {
				file, stdin = writeFile(t, t.TempDir(), "oversized.yaml", input), ""
			}
			stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(rejectAPICalls(t)),
				[]string{"alertmanager-imports", "upsert", "-f", file}, stdin)

			require.ErrorContains(t, err, "input exceeds the 10 MiB limit")
			assert.Equal(t, gcxerrors.ExitGeneralError, exitCodeOf(err))
			assert.Empty(t, stdout)
		})
	}
}

func TestAlertmanagerImportUpsert_FromDatasource(t *testing.T) {
	const (
		config = "route:\n  receiver: webhook\nreceivers:\n  - name: webhook\n    webhook_configs:\n      - url: super-secret-value\n"
		tmpl   = `{{ define "secret" }}super-secret-template{{ end }}`
	)
	proxyBody, err := yaml.Marshal(alert.AlertmanagerUserConfig{
		AlertmanagerConfig: config,
		TemplateFiles:      map[string]string{"secret.tmpl": tmpl},
	})
	require.NoError(t, err)

	setAgentMode(t, true)
	fixture := &datasourceImportFixture{
		datasourceBody: `{"name":"Prod AM.Main","type":"alertmanager","jsonData":{"implementation":"MIMIR"}}`,
		proxyBody:      string(proxyBody),
		convertBody:    mergeResponseWithRenames,
	}
	stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(serveDatasourceImportAPI(t, fixture)),
		[]string{"alertmanager-imports", "upsert", "--from-datasource", "am/uid"}, "")

	require.NoError(t, err)
	assert.NotContains(t, stdout+stderr, "super-secret-value")
	assert.NotContains(t, stdout+stderr, "super-secret-template")
	assert.Equal(t, []string{
		"/api/datasources/uid/am%2Fuid",
		"/api/datasources/proxy/uid/am%2Fuid/api/v1/alerts",
		"/api/convert/api/v1/alerts",
	}, fixture.paths)
	assert.Equal(t, "prod-am-main", fixture.convertRequest.header.Get(identifierHeader))
	var body alert.AlertmanagerUserConfig
	require.NoError(t, json.Unmarshal(fixture.convertRequest.body, &body))
	assert.Equal(t, config, body.AlertmanagerConfig)
	assert.Equal(t, map[string]string{"secret.tmpl": tmpl}, body.TemplateFiles)
	doc, ok := decodeSingleJSONDocument(t, stdout).(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "prod-am-main", doc["identifier"])
	assert.Equal(t, map[string]any{"kind": "alertmanager-import", "name": "prod-am-main"}, doc["target"])
}

func TestAlertmanagerImportUpsert_FromDatasourceExplicitIdentifierWins(t *testing.T) {
	setAgentMode(t, false)
	fixture := &datasourceImportFixture{
		datasourceBody: `{"name":"Derived Name","type":"alertmanager","jsonData":{"implementation":"mimir"}}`,
		proxyBody:      "alertmanager_config: 'route: {}'\ntemplate_files: {}\n",
		convertBody:    mergeResponseWithRenames,
	}
	stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(serveDatasourceImportAPI(t, fixture)),
		[]string{"alertmanager-imports", "upsert", "explicit", "--from-datasource", "am"}, "")

	require.NoError(t, err)
	assert.Equal(t, "explicit", fixture.convertRequest.header.Get(identifierHeader))
	assert.Contains(t, stdout, `Alertmanager configuration "explicit"`)
}

func TestAlertmanagerImportUpsert_FromDatasourceDerivedIdentifierInTextOutput(t *testing.T) {
	setAgentMode(t, false)
	fixture := &datasourceImportFixture{
		datasourceBody: `{"name":"Derived Name","type":"alertmanager","jsonData":{}}`,
		proxyBody:      "alertmanager_config: 'route: {}'\n",
		convertBody:    mergeResponseWithRenames,
	}
	stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(serveDatasourceImportAPI(t, fixture)),
		[]string{"alertmanager-imports", "upsert", "--from-datasource", "am"}, "")

	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, `Alertmanager configuration "derived-name"`)
}

func TestAlertmanagerImportIdentifierFromDatasourceName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "MixedCase", want: "mixedcase"},
		{name: "dots.and spaces", want: "dots-and-spaces"},
		{name: "---leading and trailing!!!", want: "leading-and-trailing"},
		{name: "Ünïcode AM", want: "n-code-am"},
		{name: strings.Repeat("a", 39) + "--tail", want: strings.Repeat("a", 39)},
		{name: "!@#$", want: "imported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, alert.AlertmanagerImportIdentifierFromDatasourceName(tt.name))
		})
	}
}

func TestAlertmanagerImportUpsert_FromDatasourceImplementations(t *testing.T) {
	tests := []struct {
		name           string
		implementation string
	}{
		{name: "empty", implementation: `""`},
		{name: "cortex", implementation: `"cortex"`},
		{name: "non-string", implementation: `123`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, false)
			fixture := &datasourceImportFixture{
				datasourceBody: fmt.Sprintf(`{"name":"AM","type":"alertmanager","jsonData":{"implementation":%s}}`, tt.implementation),
				proxyBody:      "alertmanager_config: 'route: {}'\n",
				convertBody:    mergeResponseWithRenames,
			}
			_, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(serveDatasourceImportAPI(t, fixture)),
				[]string{"alertmanager-imports", "upsert", "--from-datasource", "am"}, "")

			require.NoError(t, err)
		})
	}
}

func TestAlertmanagerImportUpsert_FromDatasourceErrors(t *testing.T) {
	tests := []struct {
		name           string
		datasourceCode int
		datasourceBody string
		proxyCode      int
		proxyBody      string
		wantErr        string
		wantExit       int
	}{
		{
			name:           "datasource not found",
			datasourceCode: http.StatusNotFound,
			datasourceBody: `{"message":"not found"}`,
			wantErr:        "datasource missing not found",
		},
		{
			name:           "wrong datasource type",
			datasourceBody: `{"name":"Prometheus","type":"prometheus","jsonData":{}}`,
			wantErr:        "datasource must be of type alertmanager, got prometheus",
		},
		{
			name:           "unsupported implementation",
			datasourceBody: `{"name":"Prometheus AM","type":"alertmanager","jsonData":{"implementation":"prometheus"}}`,
			wantErr: "datasource implementation prometheus is not supported; " +
				"only Mimir and Cortex Alertmanagers expose their configuration",
		},
		{
			name:           "proxy JSON 404",
			datasourceBody: `{"name":"AM","type":"alertmanager","jsonData":{}}`,
			proxyCode:      http.StatusNotFound,
			proxyBody:      `{"message":"upstream endpoint is missing"}`,
			wantErr:        "datasource proxy returned 404: upstream endpoint is missing",
		},
		{
			name:           "proxy plain 404",
			datasourceBody: `{"name":"AM","type":"alertmanager","jsonData":{}}`,
			proxyCode:      http.StatusNotFound,
			proxyBody:      "not found",
			wantErr:        "the Alertmanager behind datasource missing has no stored configuration",
		},
		{
			name:           "empty configuration",
			datasourceBody: `{"name":"AM","type":"alertmanager","jsonData":{}}`,
			proxyBody:      "alertmanager_config: ''\ntemplate_files: {}\n",
			wantErr:        "the Alertmanager behind datasource missing has no stored configuration",
		},
		{
			name:           "proxy forbidden",
			datasourceBody: `{"name":"AM","type":"alertmanager","jsonData":{}}`,
			proxyCode:      http.StatusForbidden,
			proxyBody:      `{"message":"forbidden"}`,
			wantErr:        "datasource proxy: request failed with status 403: forbidden",
			wantExit:       gcxerrors.ExitAuthFailure,
		},
		{
			name:           "proxy server error",
			datasourceBody: `{"name":"AM","type":"alertmanager","jsonData":{}}`,
			proxyCode:      http.StatusBadGateway,
			proxyBody:      strings.Repeat("x", 2048),
			wantErr:        "datasource proxy: request failed with status 502: " + strings.Repeat("x", 1024),
		},
		{
			name:           "proxy error does not split UTF-8",
			datasourceBody: `{"name":"AM","type":"alertmanager","jsonData":{}}`,
			proxyCode:      http.StatusBadGateway,
			proxyBody:      strings.Repeat("x", 1023) + "€tail",
			wantErr:        "datasource proxy: request failed with status 502: " + strings.Repeat("x", 1023),
		},
		{
			name:           "proxy JSON 404 is parsed before display truncation",
			datasourceBody: `{"name":"AM","type":"alertmanager","jsonData":{}}`,
			proxyCode:      http.StatusNotFound,
			proxyBody:      fmt.Sprintf(`{"message":%q}`, strings.Repeat("x", 2048)),
			wantErr:        "datasource proxy returned 404: " + strings.Repeat("x", 1024),
		},
		{
			name:           "non-200 success does not include body",
			datasourceBody: `{"name":"AM","type":"alertmanager","jsonData":{}}`,
			proxyCode:      http.StatusAccepted,
			proxyBody:      "super-secret-value",
			wantErr:        "datasource proxy returned 202 Accepted",
		},
		{
			name:           "redirect does not include body",
			datasourceBody: `{"name":"AM","type":"alertmanager","jsonData":{}}`,
			proxyCode:      http.StatusMultipleChoices,
			proxyBody:      "super-secret-value",
			wantErr:        "datasource proxy returned 300 Multiple Choices",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, false)
			fixture := &datasourceImportFixture{
				datasourceStatus: tt.datasourceCode,
				datasourceBody:   tt.datasourceBody,
				proxyStatus:      tt.proxyCode,
				proxyBody:        tt.proxyBody,
				convertBody:      mergeResponseWithRenames,
			}
			stdout, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(serveDatasourceImportAPI(t, fixture)),
				[]string{"alertmanager-imports", "upsert", "--from-datasource", "missing"}, "")

			require.EqualError(t, err, tt.wantErr)
			wantExit := tt.wantExit
			if wantExit == 0 {
				wantExit = gcxerrors.ExitGeneralError
			}
			assert.Equal(t, wantExit, exitCodeOf(err))
			assert.Empty(t, stdout)
			assert.Empty(t, fixture.convertRequest.body)
		})
	}
}

func TestAlertmanagerImportUpsert_FromDatasourceDryRun(t *testing.T) {
	setAgentMode(t, false)
	fixture := &datasourceImportFixture{
		datasourceBody: `{"name":"AM","type":"alertmanager","jsonData":{"implementation":"cortex"}}`,
		proxyBody:      "alertmanager_config: 'route: {}'\n",
		convertStatus:  http.StatusOK,
		convertBody:    mergeResponseWithRenames,
	}
	_, _, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(serveDatasourceImportAPI(t, fixture)),
		[]string{"alertmanager-imports", "upsert", "--from-datasource", "am", "--dry-run"}, "")

	require.NoError(t, err)
	assert.Equal(t, "true", fixture.convertRequest.header.Get(dryRunHeader))
}

func TestAlertmanagerImportUpsert_RejectsInvalidInputBeforeAPICall(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		stdin    string
		wantErr  string
		wantExit int
	}{
		{name: "missing source", args: []string{}, wantErr: "--filename / -f or --from-datasource is required", wantExit: gcxerrors.ExitUsageError},
		{name: "datasource with filename", args: []string{"-f", "input.yaml", "--from-datasource", "am"}, wantErr: "mutually exclusive", wantExit: gcxerrors.ExitUsageError},
		{name: "datasource with template", args: []string{"--from-datasource", "am", "--template", "a.tmpl"}, wantErr: "mutually exclusive", wantExit: gcxerrors.ExitUsageError},
		{name: "empty datasource UID", args: []string{"--from-datasource", ""}, wantErr: "--from-datasource UID must not be empty", wantExit: gcxerrors.ExitUsageError},
		{name: "empty stdin", args: []string{"-f", "-"}, stdin: "\n", wantErr: "input is empty", wantExit: gcxerrors.ExitGeneralError},
		{name: "empty identifier", args: []string{" ", "-f", "-"}, stdin: "route: {}", wantErr: "IDENTIFIER must not be empty", wantExit: gcxerrors.ExitUsageError},
		{name: "identifier with leading space", args: []string{" mimir-prod", "-f", "-"}, stdin: "route: {}", wantErr: "IDENTIFIER must not have leading or trailing whitespace", wantExit: gcxerrors.ExitUsageError},
		{name: "identifier with trailing space", args: []string{"mimir-prod ", "-f", "-"}, stdin: "route: {}", wantErr: "IDENTIFIER must not have leading or trailing whitespace", wantExit: gcxerrors.ExitUsageError},
		{name: "identifier with trailing tab", args: []string{"mimir-prod\t", "-f", "-"}, stdin: "route: {}", wantErr: "IDENTIFIER must not have leading or trailing whitespace", wantExit: gcxerrors.ExitUsageError},
		{name: "two identifiers", args: []string{"a", "b", "-f", "-"}, stdin: "route: {}", wantErr: "accepts at most 1 arg(s)"},
		{
			name:     "duplicate template name",
			args:     []string{"-f", "-"},
			stdin:    "alertmanager_config: 'route: {}'\ntemplate_files:\n  a.tmpl: first\n  a.tmpl: second\n",
			wantErr:  `key "a.tmpl" already set in map`,
			wantExit: gcxerrors.ExitGeneralError,
		},
		{
			name:     "duplicate template name with different quoting",
			args:     []string{"-f", "-"},
			stdin:    "alertmanager_config: 'route: {}'\ntemplate_files: {yes: first, 'yes': second}\n",
			wantErr:  `key "yes" already set in map`,
			wantExit: gcxerrors.ExitGeneralError,
		},
		{
			name:     "duplicate alertmanager_config",
			args:     []string{"-f", "-"},
			stdin:    "alertmanager_config: 'route: {}'\nalertmanager_config: 'receivers: []'\n",
			wantErr:  `key "alertmanager_config" already set in map`,
			wantExit: gcxerrors.ExitGeneralError,
		},
		{
			name:     "duplicate key nested in a plain config",
			args:     []string{"-f", "-"},
			stdin:    "receivers:\n  - name: first\n    name: second\n",
			wantErr:  `key "name" already set in map`,
			wantExit: gcxerrors.ExitGeneralError,
		},
		{
			name:     "duplicate key in JSON",
			args:     []string{"-f", "-"},
			stdin:    `{"route": {"receiver": "first", "receiver": "second"}}`,
			wantErr:  `key "receiver" already set in map`,
			wantExit: gcxerrors.ExitGeneralError,
		},
		{
			name:     "YAML merge override",
			args:     []string{"-f", "-"},
			stdin:    "route:\n  receiver: default\n  routes:\n    - &base {receiver: first}\n    - <<: *base\n      receiver: second\n",
			wantErr:  `key "receiver" already set in map`,
			wantExit: gcxerrors.ExitGeneralError,
		},
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
		{name: "promoted with dry run disabled", flags: []string{"--dry-run=false", "--promote", "--force"}, status: http.StatusAccepted, wantAction: "promoted", wantPromo: true},
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
				[]string{"alertmanager-imports", "upsert", "mimir-prod", "-f", alertmanagerFile(t), "--dry-run"}, "")

			require.NoError(t, err)
			assert.Empty(t, stderr)
			assert.NotEmpty(t, stdout)
			assert.Empty(t, got.header.Get(promoteHeader))
			assert.Equal(t, "true", got.header.Get(dryRunHeader))
		})
	}
}

func TestAlertmanagerImportUpsert_RejectsDryRunWithPromote(t *testing.T) {
	setAgentMode(t, false)
	tests := []struct {
		name string
		file string
	}{
		{name: "file", file: alertmanagerFile(t)},
		{name: "empty stdin", file: "-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := unexpectedGrafanaConfigLoad{t: t}
			stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader),
				[]string{"alertmanager-imports", "upsert", "mimir-prod", "-f", tt.file, "--dry-run", "--promote"}, "")

			require.ErrorContains(t, err, "--dry-run and --promote are mutually exclusive")
			assert.Equal(t, gcxerrors.ExitUsageError, exitCodeOf(err))
			assert.Empty(t, stdout)
			assert.Empty(t, stderr, "conflicting flags must not prompt")
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

type unexpectedGrafanaConfigLoad struct{ t *testing.T }

func (l unexpectedGrafanaConfigLoad) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	l.t.Fatal("invalid input must not load Grafana config")
	return config.NamespacedRESTConfig{}, nil
}

func TestAlertmanagerImportCommands_ValidateIdentifier(t *testing.T) {
	setAgentMode(t, false)
	commands := []struct {
		name   string
		flags  []string
		method string
	}{
		{name: "upsert", flags: []string{"-f", alertmanagerFile(t), "--promote"}, method: http.MethodPost},
		{name: "get", flags: []string{"-o", "json"}, method: http.MethodGet},
		{name: "promote", method: http.MethodPost},
		{name: "delete", method: http.MethodDelete},
	}
	tests := []struct {
		name       string
		identifier string
		wantErr    string
	}{
		{name: "empty", wantErr: "IDENTIFIER must not be empty"},
		{name: "padded", identifier: " a.b ", wantErr: "IDENTIFIER must not have leading or trailing whitespace"},
		{name: "uppercase", identifier: "Prod", wantErr: "lowercase RFC 1123 subdomain"},
		{name: "slash", identifier: "bad/name", wantErr: "lowercase RFC 1123 subdomain"},
		{name: "leading hyphen", identifier: "-lead", wantErr: "lowercase RFC 1123 subdomain"},
		{name: "non-ASCII", identifier: "prød", wantErr: "lowercase RFC 1123 subdomain"},
		{name: "too long", identifier: strings.Repeat("a", 41), wantErr: "IDENTIFIER must be at most 40 characters"},
		{name: "maximum length", identifier: strings.Repeat("a", 40)},
		{name: "dotted", identifier: "a.b"},
	}

	for _, cmd := range commands {
		for _, tt := range tests {
			t.Run(cmd.name+"/"+tt.name, func(t *testing.T) {
				var got recordedRequest
				var loader alert.GrafanaConfigLoader = unexpectedGrafanaConfigLoad{t: t}
				if tt.wantErr == "" {
					loader = serveConvertAPI(t, &got, http.StatusOK, `{"alertmanager_config":"route: {}"}`)
				}
				// Put the identifier after -- so Cobra does not treat -lead as a flag.
				args := append([]string{"alertmanager-imports", cmd.name}, cmd.flags...)
				args = append(args, "--", tt.identifier)
				stdout, stderr, err := runCmdSplit(t, alert.NewAlertmanagerImportsCommandsForTest(loader), args, "y\n")

				if tt.wantErr != "" {
					require.ErrorContains(t, err, tt.wantErr)
					assert.Equal(t, gcxerrors.ExitUsageError, exitCodeOf(err))
					assert.Empty(t, got.method)
					assert.Empty(t, stdout)
					assert.Empty(t, stderr, "invalid identifiers must not prompt")
					return
				}

				require.NoError(t, err)
				assert.Equal(t, cmd.method, got.method)
				if cmd.name == "promote" {
					assert.Contains(t, got.path, "/"+tt.identifier+"/promote")
				} else {
					assert.Equal(t, tt.identifier, got.header.Get(identifierHeader))
				}
			})
		}
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
