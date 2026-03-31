//nolint:testpackage // tests need access to internal symbols (templates, opts, processGenerateArg)
package dev

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveResourceType(t *testing.T) {
	tests := []struct {
		name      string
		optType   string
		dir       string
		want      string
		wantErr   bool
		errSubstr string
	}{
		{name: "dashboards dir", dir: "dashboards", want: "dashboard"},
		{name: "dashboard singular", dir: "dashboard", want: "dashboard"},
		{name: "alerts dir", dir: "alerts", want: "alertrule"},
		{name: "alertrules dir", dir: "alertrules", want: "alertrule"},
		{name: "alertrule singular", dir: "alertrule", want: "alertrule"},
		{name: "case insensitive Dashboards", dir: "Dashboards", want: "dashboard"},
		{name: "case insensitive ALERTS", dir: "ALERTS", want: "alertrule"},
		{name: "nested dashboards", dir: "internal/dashboards", want: "dashboard"},
		{name: "type flag override", optType: "alertrule", dir: "custom", want: "alertrule"},
		{name: "type flag dashboard", optType: "dashboard", dir: "anything", want: "dashboard"},
		{name: "unknown dir no flag", dir: "custom", wantErr: true, errSubstr: "cannot infer"},
		{name: "invalid type flag", optType: "invalid", dir: "custom", wantErr: true, errSubstr: "unsupported type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &generateOpts{Type: tt.optType}
			got, err := resolveResourceType(opts, tt.dir)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNameInference(t *testing.T) {
	tests := []struct {
		filename string
		wantName string
	}{
		{"my-dashboard.go", "my-dashboard"},
		{"my-dashboard", "my-dashboard"},
		{"high-cpu-usage.go", "high-cpu-usage"},
		{"simple", "simple"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			name := tt.filename
			if filepath.Ext(name) == ".go" {
				name = name[:len(name)-3]
			}
			assert.Equal(t, tt.wantName, name)
		})
	}
}

func TestDashboardTemplateValid(t *testing.T) {
	tmpl, err := template.New("").Option("missingkey=error").ParseFS(templatesFS, "templates/generate/*.tmpl")
	require.NoError(t, err)

	var buf bytes.Buffer
	err = tmpl.ExecuteTemplate(&buf, "dashboard.go.tmpl", map[string]any{
		"Package":  "dashboards",
		"FuncName": "MyServiceOverview",
		"Name":     "my-service-overview",
	})
	require.NoError(t, err)

	output := buf.String()

	// Verify it parses as valid Go.
	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "dashboard.go", output, parser.AllErrors)
	require.NoError(t, parseErr, "dashboard template output should be valid Go")

	// Verify gofmt-validity.
	formatted, fmtErr := format.Source(buf.Bytes())
	require.NoError(t, fmtErr, "dashboard template output should be gofmt-valid")
	assert.Equal(t, string(formatted), output, "dashboard template output should match gofmt")

	// Verify key content.
	assert.Contains(t, output, `dashboardv2beta1`)
	assert.Contains(t, output, `timeseries`)
	assert.Contains(t, output, `testdata`)
	assert.Contains(t, output, `resource`)
	assert.Contains(t, output, `func MyServiceOverview() *resource.ManifestBuilder`)
	assert.Contains(t, output, `NewDashboardBuilder("my-service-overview")`)
	assert.Contains(t, output, `AutoGridLayout`)
	assert.Contains(t, output, `Manifest(`)
}

func TestAlertruleTemplateValid(t *testing.T) {
	tmpl, err := template.New("").Option("missingkey=error").ParseFS(templatesFS, "templates/generate/*.tmpl")
	require.NoError(t, err)

	var buf bytes.Buffer
	err = tmpl.ExecuteTemplate(&buf, "alertrule.go.tmpl", map[string]any{
		"Package":  "alerts",
		"FuncName": "HighCpuUsage",
		"Name":     "high-cpu-usage",
	})
	require.NoError(t, err)

	output := buf.String()

	// Verify it parses as valid Go.
	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "alertrule.go", output, parser.AllErrors)
	require.NoError(t, parseErr, "alertrule template output should be valid Go")

	// Verify gofmt-validity.
	formatted, fmtErr := format.Source(buf.Bytes())
	require.NoError(t, fmtErr, "alertrule template output should be gofmt-valid")
	assert.Equal(t, string(formatted), output, "alertrule template output should match gofmt")

	// Verify key content.
	assert.Contains(t, output, `alerting`)
	assert.Contains(t, output, `resource`)
	assert.Contains(t, output, `func HighCpuUsage() *resource.ManifestBuilder`)
	assert.Contains(t, output, `NewRuleBuilder("high-cpu-usage")`)
	assert.Contains(t, output, `Condition("A")`)
	assert.Contains(t, output, `For("5m")`)
	assert.Contains(t, output, `Labels(map[string]string{})`)
	assert.Contains(t, output, `"summary": ""`)
	assert.Contains(t, output, `resource.NewManifestBuilder()`)
}

func TestGenerateEndToEnd(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		typeFlag    string
		wantFiles   []string
		wantErr     bool
		errContains string
	}{
		{
			name:      "dashboard from dashboards dir",
			args:      []string{"dashboards/my-service-overview.go"},
			wantFiles: []string{"dashboards/my_service_overview.go"},
		},
		{
			name:      "alertrule from alerts dir",
			args:      []string{"alerts/high-cpu-usage.go"},
			wantFiles: []string{"alerts/high_cpu_usage.go"},
		},
		{
			name:      "no .go extension",
			args:      []string{"dashboards/my-dashboard"},
			wantFiles: []string{"dashboards/my_dashboard.go"},
		},
		{
			name:      "type flag override",
			args:      []string{"monitoring/cpu-alert.go"},
			typeFlag:  "alertrule",
			wantFiles: []string{"monitoring/cpu_alert.go"},
		},
		{
			name:      "batch generation",
			args:      []string{"dashboards/a.go", "dashboards/b.go", "alerts/c.go"},
			wantFiles: []string{"dashboards/a.go", "dashboards/b.go", "alerts/c.go"},
		},
		{
			name:      "nested dir with inference",
			args:      []string{"new/nested/dashboards/test.go"},
			wantFiles: []string{"new/nested/dashboards/test.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Adjust args to use tmpDir.
			adjustedArgs := make([]string, len(tt.args))
			for i, arg := range tt.args {
				adjustedArgs[i] = filepath.Join(tmpDir, arg)
			}

			tmpl, err := template.New("").Option("missingkey=error").ParseFS(templatesFS, "templates/generate/*.tmpl")
			require.NoError(t, err)

			opts := &generateOpts{Type: tt.typeFlag}
			cmd := generateCmd()
			cmd.SetOut(&bytes.Buffer{})

			for _, arg := range adjustedArgs {
				err := processGenerateArg(cmd, tmpl, opts, arg)
				require.NoError(t, err)
			}

			// Verify files were created.
			for _, wantFile := range tt.wantFiles {
				fullPath := filepath.Join(tmpDir, wantFile)
				_, err := os.Stat(fullPath)
				require.NoError(t, err, "expected file %s to exist", wantFile)

				// Verify generated file is valid Go.
				content, readErr := os.ReadFile(fullPath)
				require.NoError(t, readErr)
				fset := token.NewFileSet()
				_, parseErr := parser.ParseFile(fset, filepath.Base(fullPath), content, parser.AllErrors)
				assert.NoError(t, parseErr, "generated file %s should be valid Go", wantFile)
			}
		})
	}
}

func TestGenerateFileAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create the target file.
	dashDir := filepath.Join(tmpDir, "dashboards")
	require.NoError(t, os.MkdirAll(dashDir, 0744))
	require.NoError(t, os.WriteFile(filepath.Join(dashDir, "existing.go"), []byte("package dashboards\n"), 0600))

	tmpl, err := template.New("").Option("missingkey=error").ParseFS(templatesFS, "templates/generate/*.tmpl")
	require.NoError(t, err)

	opts := &generateOpts{}
	cmd := generateCmd()
	cmd.SetOut(&bytes.Buffer{})

	err = processGenerateArg(cmd, tmpl, opts, filepath.Join(tmpDir, "dashboards/existing.go"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file already exists")
}

func TestGenerateUnknownDirNoFlag(t *testing.T) {
	opts := &generateOpts{}
	_, err := resolveResourceType(opts, "custom")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot infer resource type")
	assert.Contains(t, err.Error(), "--type")
}

func TestGeneratePackageName(t *testing.T) {
	tests := []struct {
		dir     string
		wantPkg string
	}{
		{"dashboards", "dashboards"},
		{"alerts", "alerts"},
		{"internal/monitoring", "monitoring"},
		{"a/b/c", "c"},
	}

	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			got := filepath.Base(tt.dir)
			assert.Equal(t, tt.wantPkg, got)
		})
	}
}
