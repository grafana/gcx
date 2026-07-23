package dev

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/strcase"
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
	IO   cmdio.Options
	Type string
}

func (opts *generateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&opts.Type, "type", "t", "", "Resource type to generate (dashboard, alertrule). Overrides directory-based inference.")
	// The generate result is an ArtifactReceipt document through the codec
	// system: the default text codec prints the familiar per-file and
	// summary lines; agent mode and explicit -o json/yaml get the
	// structured document.
	opts.IO.RegisterCustomCodec("text", &generateReceiptCodec{})
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(flags)
}

func (opts *generateOpts) Validate() error {
	return opts.IO.Validate()
}

// generateReceiptCodec is the human "text" codec for the generate receipt:
// it renders exactly the per-file "Generated <file>" lines and the final
// "Generated N file(s)." summary the command has always printed, so default
// human stdout stays byte-identical to the pre-codec output (per-file error
// lines are diagnostics and moved to stderr).
type generateReceiptCodec struct{}

func (c *generateReceiptCodec) Format() format.Format { return "text" }

func (c *generateReceiptCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *generateReceiptCodec) Encode(w io.Writer, value any) error {
	receipt, ok := value.(cmdio.ArtifactReceipt)
	if !ok {
		return errors.New("invalid data type for generate receipt codec: expected ArtifactReceipt")
	}

	for _, file := range receipt.Files {
		cmdio.Success(w, "Generated %s", file.Path)
	}

	if receipt.Summary.Failed > 0 {
		cmdio.Info(w, "Generated %d file(s), %d failed.", receipt.Summary.Succeeded, receipt.Summary.Failed)
	} else {
		cmdio.Info(w, "Generated %d file(s).", receipt.Summary.Succeeded)
	}

	return nil
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
	if err := opts.Validate(); err != nil {
		return err
	}

	tmpl, err := template.New("").Option("missingkey=error").ParseFS(templatesFS, "templates/generate/*.tmpl")
	if err != nil {
		return fmt.Errorf("parsing templates: %w", err)
	}

	receipt := generateFiles(tmpl, opts, args, cmd.ErrOrStderr())

	// Total failure: no receipt — exit 4 would misreport a complete failure
	// as partial. The raw error takes the standard path (one gcx.error
	// document in agent mode, exit 1), matching the batch cohort's
	// zero-success convention.
	if receipt.Summary.Failed > 0 && receipt.Summary.Succeeded == 0 {
		return receiptFailuresError(receipt.Failures)
	}

	if err := opts.IO.Encode(cmd.OutOrStdout(), receipt); err != nil {
		return err
	}

	if receipt.Summary.Failed > 0 {
		// The receipt (with enumerated failures) is already on stdout —
		// EmittedError carries exit 4 without a second error document.
		return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure,
			gcxerrors.NewPartialFailureError("generate",
				receipt.Summary.Succeeded+receipt.Summary.Failed, receipt.Summary.Failed))
	}

	return nil
}

// receiptFailuresError joins an artifact receipt's enumerated failures into
// one error for the zero-success path.
func receiptFailuresError(failures []cmdio.MutationFailure) error {
	errs := make([]error, 0, len(failures))
	for _, f := range failures {
		msg := f.Error
		if f.Target.Name != "" {
			msg = f.Target.Name + ": " + msg
		}
		errs = append(errs, errors.New(msg))
	}
	return errors.Join(errs...)
}

// generateFiles processes each argument, streaming per-file failure notes to
// warn (stderr — diagnostics, not results), and returns the artifact receipt:
// written files, counts, and enumerated failures.
func generateFiles(tmpl *template.Template, opts *generateOpts, args []string, warn io.Writer) cmdio.ArtifactReceipt {
	receipt := cmdio.NewArtifactReceipt("generated", "go")

	for _, arg := range args {
		outputFile, resourceType, err := processGenerateArg(tmpl, opts, arg)
		if err != nil {
			cmdio.Error(warn, "%s: %s", arg, err)
			receipt.Summary.Failed++
			receipt.Failures = append(receipt.Failures, cmdio.MutationFailure{
				Target: cmdio.MutationTarget{Name: arg},
				Error:  err.Error(),
			})
			continue
		}

		receipt.Summary.Succeeded++
		receipt.Files = append(receipt.Files, cmdio.ArtifactFile{Path: outputFile, Kind: resourceType})
	}

	return receipt
}

// processGenerateArg generates one stub file and returns its path and
// resource type.
func processGenerateArg(tmpl *template.Template, opts *generateOpts, arg string) (string, string, error) {
	dir := filepath.Dir(arg)
	base := filepath.Base(arg)

	// Infer resource name from filename (strip .go extension if present).
	name := strings.TrimSuffix(base, ".go")
	if name == "" {
		return "", "", errors.New("empty filename")
	}

	// Resolve resource type.
	resourceType, err := resolveResourceType(opts, dir)
	if err != nil {
		return "", "", err
	}

	// Normalize output path: snake_case filename with .go extension.
	outputFile := filepath.Join(dir, strcase.ToSnakeCase(name)+".go")

	// Check if file already exists.
	if _, err := os.Stat(outputFile); err == nil {
		return "", "", fmt.Errorf("file already exists: %s. Delete it first or use a different name", outputFile)
	}

	// Ensure output directory exists.
	if err := ensureDirectory(filepath.Dir(outputFile)); err != nil {
		return "", "", fmt.Errorf("creating directory: %w", err)
	}

	// Derive package name from immediate parent directory.
	packageName := strings.ToLower(filepath.Base(dir))

	// Select and execute template.
	templateName := typeToTemplate[resourceType]
	data := map[string]any{
		"Package":  packageName,
		"FuncName": strcase.ToPascalCase(name),
		"Name":     name,
	}

	fileHandle, err := os.OpenFile(outputFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", "", fmt.Errorf("creating file: %w", err)
	}
	defer fileHandle.Close()

	if err := tmpl.ExecuteTemplate(fileHandle, templateName, data); err != nil {
		return "", "", fmt.Errorf("executing template: %w", err)
	}

	return outputFile, resourceType, nil
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
