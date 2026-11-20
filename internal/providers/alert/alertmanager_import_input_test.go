package alert_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainAlertmanagerConfig keeps comments, key order and indentation that a
// re-serialisation would lose, so tests can see that gcx sends the bytes
// unchanged.
const plainAlertmanagerConfig = `# Production Alertmanager
route:
  receiver: webhook   # default receiver
  group_by: [alertname]
receivers:
  - name: webhook
    webhook_configs:
      - url: 'https://example.com/webhook'
`

const mimirUserConfig = `template_files:
  default.tmpl: '{{ define "custom" }}Custom message{{ end }}'
alertmanager_config: |
  route:
    receiver: webhook
  receivers:
    - name: webhook
`

// writeFile writes content to dir/name and returns the path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestReadAlertmanagerUserConfig_Formats(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  alert.AlertmanagerUserConfig
	}{
		{
			name:  "plain alertmanager config is sent byte for byte",
			input: plainAlertmanagerConfig,
			want:  alert.AlertmanagerUserConfig{AlertmanagerConfig: plainAlertmanagerConfig},
		},
		{
			name:  "plain alertmanager config in JSON is sent byte for byte",
			input: `{"route": {"receiver": "webhook"}, "receivers": [{"name": "webhook"}]}`,
			want: alert.AlertmanagerUserConfig{
				AlertmanagerConfig: `{"route": {"receiver": "webhook"}, "receivers": [{"name": "webhook"}]}`,
			},
		},
		{
			name:  "mimir user config in YAML accepts extra fields",
			input: mimirUserConfig + "metadata: {source: mimir}\n",
			want: alert.AlertmanagerUserConfig{
				AlertmanagerConfig: "route:\n  receiver: webhook\nreceivers:\n  - name: webhook\n",
				TemplateFiles:      map[string]string{"default.tmpl": `{{ define "custom" }}Custom message{{ end }}`},
			},
		},
		{
			name:  "mimir user config in JSON without templates",
			input: `{"alertmanager_config": "route:\n  receiver: webhook\n"}`,
			want:  alert.AlertmanagerUserConfig{AlertmanagerConfig: "route:\n  receiver: webhook\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, t.TempDir(), "input.yaml", tt.input)

			got, err := alert.ReadAlertmanagerUserConfig(path, nil, nil)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReadAlertmanagerUserConfig_TemplateNameCompatibility(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{
			name: "template-only merges and repeated merge keys",
			input: `template_files:
  <<: {yes: first}
  <<: [{on: second}, {1.0: third}]
`,
			want: map[string]string{"yes": "first", "on": "second", "1.0": "third"},
		},
		{
			name: "shared alias keeps legacy key handling",
			input: `metadata: &shared {yes: first}
template_files: *shared
`,
			want: map[string]string{"true": "first"},
		},
		{
			name:  "null template contents",
			input: "template_files: {yes: first, x.tmpl: null}\n",
			want:  map[string]string{"yes": "first", "x.tmpl": ""},
		},
		{
			name:  "null template map",
			input: "template_files: null\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := alert.ReadAlertmanagerUserConfig("-", strings.NewReader("alertmanager_config: 'route: {}'\n"+tt.input), nil)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.TemplateFiles)
		})
	}
}

func TestReadAlertmanagerUserConfig_ReadsStdin(t *testing.T) {
	got, err := alert.ReadAlertmanagerUserConfig("-", strings.NewReader(plainAlertmanagerConfig), nil)

	require.NoError(t, err)
	assert.Equal(t, plainAlertmanagerConfig, got.AlertmanagerConfig)
}

func TestReadAlertmanagerUserConfig_InvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "empty input", input: " \n\t\n", wantErr: "input is empty"},
		{name: "list document", input: "- route: {}\n", wantErr: "failed to parse input"},
		{name: "scalar document", input: "route\n", wantErr: "failed to parse input"},
		{name: "null document", input: "null\n", wantErr: "input is not an object"},
		{
			name:    "alertmanager_config is an object",
			input:   "alertmanager_config:\n  route:\n    receiver: webhook\n",
			wantErr: "alertmanager_config must be a non-empty string",
		},
		{
			name:    "template_files without alertmanager_config",
			input:   "template_files:\n  a.tmpl: x\n",
			wantErr: "alertmanager_config must be a non-empty string",
		},
		{
			name:    "alertmanager_config is a number",
			input:   "alertmanager_config: 10\ntemplate_files: {yes: first}\n",
			wantErr: "alertmanager_config must be a non-empty string",
		},
		{
			name:    "alertmanager_config is null",
			input:   "alertmanager_config: null\ntemplate_files: {yes: first}\n",
			wantErr: "alertmanager_config must be a non-empty string",
		},
		{
			name:    "invalid explicit tag on a template name",
			input:   "alertmanager_config: 'route: {}'\ntemplate_files: {!!int x: first, yes: second}\n",
			wantErr: "cannot decode !!str `x` as a !!int",
		},
		{
			name:    "template contents are a boolean",
			input:   "alertmanager_config: 'route: {}'\ntemplate_files: {yes: on}\n",
			wantErr: "template_files must map template file names to their contents: json: cannot unmarshal bool",
		},
		{
			name:    "template contents are a number",
			input:   "alertmanager_config: 'route: {}'\ntemplate_files: {yes: 10}\n",
			wantErr: "template_files must map template file names to their contents: json: cannot unmarshal number",
		},
		{
			name:    "shared alias keeps legacy duplicate check",
			input:   "alertmanager_config: 'route: {}'\nmetadata: &shared {yes: first, on: second}\ntemplate_files: *shared\n",
			wantErr: "key true already set in map",
		},
		{
			name:    "template merge override is rejected",
			input:   "alertmanager_config: 'route: {}'\ntemplate_files: {<<: {yes: first}, yes: second}\n",
			wantErr: "key true already set in map",
		},
		{
			name:    "template_files is a list",
			input:   "alertmanager_config: 'route: {}'\ntemplate_files: [a.tmpl]\n",
			wantErr: "template_files must map template file names to their contents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := alert.ReadAlertmanagerUserConfig("-", strings.NewReader(tt.input), nil)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestReadAlertmanagerUserConfig_RequiresFilename(t *testing.T) {
	_, err := alert.ReadAlertmanagerUserConfig("", strings.NewReader(plainAlertmanagerConfig), nil)

	require.EqualError(t, err, "--filename is required (use - to read from stdin)")
}

func TestReadAlertmanagerUserConfig_Templates(t *testing.T) {
	dir := t.TempDir()
	input := writeFile(t, dir, "alertmanager.yaml", plainAlertmanagerConfig)
	first := writeFile(t, dir, "first.tmpl", "first")
	second := writeFile(t, dir, "second.tmpl", "second")

	got, err := alert.ReadAlertmanagerUserConfig(input, nil, []string{first, second})

	require.NoError(t, err)
	assert.Equal(t, plainAlertmanagerConfig, got.AlertmanagerConfig)
	assert.Equal(t, map[string]string{"first.tmpl": "first", "second.tmpl": "second"}, got.TemplateFiles)
}

func TestReadAlertmanagerUserConfig_TemplatesMergeWithUserConfig(t *testing.T) {
	dir := t.TempDir()
	input := writeFile(t, dir, "user.yaml", mimirUserConfig)
	extra := writeFile(t, dir, "extra.tmpl", "extra")

	got, err := alert.ReadAlertmanagerUserConfig(input, nil, []string{extra})

	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"default.tmpl": `{{ define "custom" }}Custom message{{ end }}`,
		"extra.tmpl":   "extra",
	}, got.TemplateFiles)
}

func TestReadAlertmanagerUserConfig_TemplateErrors(t *testing.T) {
	dir := t.TempDir()
	plainInput := writeFile(t, dir, "alertmanager.yaml", plainAlertmanagerConfig)
	userInput := writeFile(t, dir, "user.yaml", mimirUserConfig)
	first := writeFile(t, t.TempDir(), "same.tmpl", "first")
	second := writeFile(t, t.TempDir(), "same.tmpl", "second")
	colliding := writeFile(t, t.TempDir(), "default.tmpl", "colliding")
	templateDir := t.TempDir()

	tests := []struct {
		name      string
		input     string
		templates []string
		wantErr   string
		wantErrIs error
	}{
		{
			name:      "same template name in two --template paths",
			input:     plainInput,
			templates: []string{first, second},
			wantErr:   `template "same.tmpl" from ` + second + " is already defined by " + first,
		},
		{
			name:      "--template collides with template_files of the input",
			input:     userInput,
			templates: []string{colliding},
			wantErr:   `template "default.tmpl" from ` + colliding + " is already defined by template_files in " + userInput,
		},
		{
			name:      "missing template path",
			input:     plainInput,
			templates: []string{filepath.Join(dir, "missing.tmpl")},
			wantErr:   "--template " + filepath.Join(dir, "missing.tmpl") + ": ",
			wantErrIs: fs.ErrNotExist,
		},
		{
			name:      "template directory",
			input:     plainInput,
			templates: []string{templateDir},
			wantErr:   "--template " + templateDir + ": is a directory; pass template files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := alert.ReadAlertmanagerUserConfig(tt.input, nil, tt.templates)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			if tt.wantErrIs != nil {
				assert.ErrorIs(t, err, tt.wantErrIs)
			}
		})
	}
}
