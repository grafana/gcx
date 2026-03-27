package dev

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/huandu/xstrings"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// typeFromDir maps directory names (lowercased) to resource types.
//
//nolint:gochecknoglobals
var typeFromDir = map[string]string{
	"dashboards": "dashboard",
	"dashboard":  "dashboard",
	"alerts":     "alertrule",
	"alertrules": "alertrule",
	"alertrule":  "alertrule",
}

//nolint:gochecknoglobals
var typeToTemplate = map[string]string{
	"dashboard": "dashboard.go.tmpl",
	"alertrule": "alertrule.go.tmpl",
}

type generateOpts struct {
	Type string
}

func (opts *generateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&opts.Type, "type", "t", "", "Resource type to generate (dashboard, alertrule). Overrides directory-based inference.")
}

func generateCmd() *cobra.Command {
	opts := &generateOpts{}

	cmd := &cobra.Command{
		Use:   "generate [FILE_PATH]...",
		Args:  cobra.MinimumNArgs(1),
		Short: "Generate typed Go stubs for Grafana resources",
		Long: `Generate typed Go code stubs using grafana-foundation-sdk builder types.

The resource type is inferred from the immediate parent directory name:
  dashboards/  → dashboard
  alerts/      → alertrule
  alertrules/  → alertrule

The resource name is inferred from the filename (without .go extension).
Use --type to override type inference when the directory name does not match.`,
		Example: `  # Generate a dashboard stub
  gcx dev generate dashboards/my-service-overview.go

  # Generate an alert rule stub
  gcx dev generate alerts/high-cpu-usage.go

  # Generate multiple stubs at once
  gcx dev generate dashboards/a.go dashboards/b.go alerts/c.go

  # Override type inference with --type
  gcx dev generate internal/monitoring/cpu-alert.go --type alertrule`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerate(cmd, opts, args)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func runGenerate(cmd *cobra.Command, opts *generateOpts, args []string) error {
	tmpl, err := template.New("").Option("missingkey=error").ParseFS(templatesFS, "templates/generate/*.tmpl")
	if err != nil {
		return fmt.Errorf("parsing templates: %w", err)
	}

	succeeded := 0
	failed := 0

	for _, arg := range args {
		if err := processGenerateArg(cmd, tmpl, opts, arg); err != nil {
			cmdio.Error(cmd.OutOrStdout(), "%s: %s", arg, err)
			failed++
		} else {
			succeeded++
		}
	}

	if failed > 0 {
		cmdio.Info(cmd.OutOrStdout(), "Generated %d file(s), %d failed.", succeeded, failed)
	} else {
		cmdio.Info(cmd.OutOrStdout(), "Generated %d file(s).", succeeded)
	}

	return nil
}

func processGenerateArg(cmd *cobra.Command, tmpl *template.Template, opts *generateOpts, arg string) error {
	dir := filepath.Dir(arg)
	base := filepath.Base(arg)

	// Infer resource name from filename (strip .go extension if present).
	name := strings.TrimSuffix(base, ".go")
	if name == "" {
		return errors.New("empty filename")
	}

	// Resolve resource type.
	resourceType, err := resolveResourceType(opts, dir)
	if err != nil {
		return err
	}

	// Normalize output path: snake_case filename with .go extension.
	outputFile := filepath.Join(dir, xstrings.ToSnakeCase(name)+".go")

	// Check if file already exists.
	if _, err := os.Stat(outputFile); err == nil {
		return fmt.Errorf("file already exists: %s. Delete it first or use a different name", outputFile)
	}

	// Ensure output directory exists.
	if err := ensureDirectory(filepath.Dir(outputFile)); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Derive package name from immediate parent directory.
	packageName := strings.ToLower(filepath.Base(dir))

	// Select and execute template.
	templateName := typeToTemplate[resourceType]
	data := map[string]any{
		"Package":  packageName,
		"FuncName": xstrings.ToPascalCase(name),
		"Name":     name,
	}

	fileHandle, err := os.OpenFile(outputFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer fileHandle.Close()

	if err := tmpl.ExecuteTemplate(fileHandle, templateName, data); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	cmdio.Success(cmd.OutOrStdout(), "Generated %s", outputFile)

	return nil
}

func resolveResourceType(opts *generateOpts, dir string) (string, error) {
	if opts.Type != "" {
		t := strings.ToLower(opts.Type)
		if _, ok := typeToTemplate[t]; !ok {
			return "", fmt.Errorf("unsupported type %q. Supported types: dashboard, alertrule", opts.Type)
		}
		return t, nil
	}

	dirName := strings.ToLower(filepath.Base(dir))
	if t, ok := typeFromDir[dirName]; ok {
		return t, nil
	}

	return "", fmt.Errorf(
		"cannot infer resource type from directory %q. "+
			"Supported directory names: dashboards, alerts, alertrules. "+
			"Use --type to specify the resource type explicitly",
		filepath.Base(dir),
	)
}
