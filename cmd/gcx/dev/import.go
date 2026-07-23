package dev

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"text/template"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	model "github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/strcase"
	"github.com/grafana/grafana-foundation-sdk/go/cog/plugins"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/dashboardv2beta1"
	"github.com/grafana/grafana-foundation-sdk/go/folderv1beta1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/tools/imports"
)

// errNoConverter marks resources whose kind/version has no Go builder
// converter — an expected capability gap (counted as skipped), as opposed to
// a real conversion failure (counted as failed, non-zero exit).
var errNoConverter = errors.New("no converter found")

//nolint:gochecknoglobals
var convertersMap = map[string]resourceConverter{
	"Dashboard.dashboard.grafana.app/v0alpha1": dashboardv1Converter,
	"Dashboard.dashboard.grafana.app/v1":       dashboardv1Converter,
	"Dashboard.dashboard.grafana.app/v1beta1":  dashboardv1Converter,
	"Dashboard.dashboard.grafana.app/v2beta1":  dashboardv2Converter,
	"Folder.folder.grafana.app/v1":             folderConverter,
}

type importOpts struct {
	IO   cmdio.Options
	Path string
}

func (opts *importOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&opts.Path, "path", "p", "imported", "Import path.")
	// The import result is an ArtifactReceipt document through the codec
	// system: the default text codec prints the familiar one-line summary;
	// agent mode and explicit -o json/yaml get the structured document.
	opts.IO.RegisterCustomCodec("text", &importReceiptCodec{})
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(flags)
}

func (opts *importOpts) Validate() error {
	return opts.IO.Validate()
}

// importReceiptCodec is the human "text" codec for the import receipt: it
// renders exactly the one-line "Imported N resources in <path>" summary the
// command has always printed, so default human stdout stays byte-identical
// to the pre-codec output.
type importReceiptCodec struct{}

func (c *importReceiptCodec) Format() format.Format { return "text" }

func (c *importReceiptCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *importReceiptCodec) Encode(w io.Writer, value any) error {
	receipt, ok := value.(cmdio.ArtifactReceipt)
	if !ok {
		return errors.New("invalid data type for import receipt codec: expected ArtifactReceipt")
	}

	cmdio.Success(w, "Imported %d resources in %s", receipt.Summary.Succeeded, receipt.Dir)
	return nil
}

func importCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &importOpts{}

	cmd := &cobra.Command{
		Use:   "import [RESOURCE_SELECTOR]...",
		Args:  cobra.ArbitraryArgs,
		Short: "Import resources from Grafana and convert them to Go builder code",
		Long:  "Import resources from a Grafana instance and convert them into Go files using the grafana-foundation-sdk builder pattern. Each imported resource is written as a function returning *resource.ManifestBuilder.",
		Example: `
	# Import all dashboards into the default path (imported/):
	gcx dev import dashboards

	# Import a specific dashboard by name:
	gcx dev import dashboards/my-dashboard

	# Import multiple resource types:
	gcx dev import dashboards folders

	# Import into a custom directory:
	gcx dev import dashboards --path src/grafana
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if err := opts.Validate(); err != nil {
				return err
			}

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			res, err := resources.FetchResources(ctx, resources.FetchRequest{
				Config:      cfg,
				StopOnError: true,
			}, args)
			if err != nil {
				return err
			}

			plugins.RegisterDefaultPlugins()

			receipt, err := importResources(&res.Resources, opts.Path, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			// Total failure: no receipt — exit 4 would misreport a complete
			// failure as partial. The raw error takes the standard path
			// (one gcx.error document in agent mode, exit 1), matching the
			// batch cohort's zero-success convention.
			if receipt.Summary.Failed > 0 && receipt.Summary.Succeeded == 0 {
				return receiptFailuresError(receipt.Failures)
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), receipt); err != nil {
				return err
			}

			if receipt.Summary.Failed > 0 {
				// The receipt (with enumerated failures) is already on
				// stdout — EmittedError carries exit 4 without a second
				// error document.
				return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure,
					gcxerrors.NewPartialFailureError("import",
						receipt.Summary.Succeeded+receipt.Summary.Failed, receipt.Summary.Failed))
			}

			return nil
		},
	}

	opts.setup(cmd.Flags())
	configOpts.BindFlags(cmd.Flags())

	return cmd
}

// importResources converts each fetched resource into a Go builder file under
// path. Per-resource skip/failure notes stream to warn (stderr — diagnostics,
// not results); the returned ArtifactReceipt is the terminal result: written
// files, counts, and enumerated failures. Resources without a registered
// converter are an expected capability gap and count as skipped; any other
// conversion error counts as failed.
func importResources(list *model.Resources, path string, warn io.Writer) (cmdio.ArtifactReceipt, error) {
	receipt := cmdio.NewArtifactReceipt("imported", "go")
	receipt.Dir = path

	err := list.ForEach(func(resource *model.Resource) error {
		file, err := convertResource(path, resource)
		if err != nil {
			resourceId := fmt.Sprintf("%s.%s", resource.Kind(), resource.Name())
			cmdio.Info(warn, "Skipping resource '%s': %s", resourceId, err)

			if errors.Is(err, errNoConverter) {
				receipt.Summary.Skipped++
				return nil
			}

			receipt.Summary.Failed++
			receipt.Failures = append(receipt.Failures, cmdio.MutationFailure{
				Target: cmdio.MutationTarget{Kind: resource.Kind(), Name: resource.Name()},
				Error:  err.Error(),
			})
			return nil
		}

		receipt.Summary.Succeeded++
		receipt.Files = append(receipt.Files, cmdio.ArtifactFile{Path: file, Kind: resource.Kind()})
		return nil
	})

	return receipt, err
}

type resourceConverter func(resource *model.Resource) (string, error)

// sdkImportOverrides maps package identifiers referenced by foundation-sdk
// converter output to their subpath within the SDK module when that subpath
// is not simply go/<identifier>. These are the SDK's only nested packages.
//
//nolint:gochecknoglobals
var sdkImportOverrides = map[string]string{
	"variants": "cog/variants",
	"plugins":  "cog/plugins",
}

func sdkImportPath(ident string) string {
	sub := ident
	if override, ok := sdkImportOverrides[ident]; ok {
		sub = override
	}
	return "github.com/grafana/grafana-foundation-sdk/go/" + sub
}

// computeSDKImports parses generated source and returns the import paths its
// selector expressions need. Foundation-sdk converters emit builder code that
// references SDK packages (cog, common, panel and query packages, …) without
// any import information, and goimports cannot resolve them reliably — the
// module cache offers several candidates for e.g. "cog" — so the importer
// derives the import list from actual usage instead. Selector roots that
// resolve to declarations in the file (locals) or to predeclared identifiers
// are ignored.
func computeSDKImports(src []byte) ([]string, error) {
	fset := token.NewFileSet()
	// Object resolution is what lets us tell locals apart from package
	// references below; it is on by default in parser.ParseFile.
	file, err := parser.ParseFile(fset, "generated.go", src, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing generated code: %w", err)
	}

	idents := map[string]struct{}{}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		// ast.Object is deprecated but still populated; a nil Obj on a
		// selector root means "not declared in this file", i.e. a package
		// reference.
		if ident.Obj != nil || types.Universe.Lookup(ident.Name) != nil {
			return true
		}
		idents[ident.Name] = struct{}{}
		return true
	})

	paths := make([]string, 0, len(idents))
	for ident := range idents {
		paths = append(paths, sdkImportPath(ident))
	}
	sort.Strings(paths)

	return paths, nil
}

// convertResource renders one resource as a Go builder file and returns the
// written file path.
func convertResource(destinationRoot string, resource *model.Resource) (string, error) {
	tmpl, err := template.New("").Option("missingkey=error").ParseFS(templatesFS, "templates/import/*.tmpl")
	if err != nil {
		return "", err
	}

	gvk := resource.GroupVersionKind()
	converterKey := fmt.Sprintf("%s.%s", gvk.Kind, gvk.GroupVersion().String())

	converter, ok := convertersMap[converterKey]
	if !ok {
		return "", fmt.Errorf("%w for %s", errNoConverter, converterKey)
	}

	converted, err := converter(resource)
	if err != nil {
		return "", err
	}

	convertedFile := filepath.Join(destinationRoot, strcase.ToSnakeCase(resource.Name())) + ".go"

	if err := ensureDirectory(filepath.Dir(convertedFile)); err != nil {
		return "", err
	}

	templateData := map[string]any{
		"Package":          filepath.Base(destinationRoot),
		"GroupVersion":     gvk.GroupVersion().String(),
		"Kind":             resource.Kind(),
		"Name":             resource.Name(),
		"FuncName":         strcase.ToPascalCase(resource.Name()),
		"ConvertedBuilder": converted,
		"Imports":          []string(nil),
	}

	// First render without imports to discover, from the code itself, which
	// SDK packages the converter output references; then render again with
	// the complete import block so the file compiles regardless of whether
	// goimports can resolve anything.
	var scanBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&scanBuf, "resource.go.tmpl", templateData); err != nil {
		return "", err
	}

	sdkImports, err := computeSDKImports(scanBuf.Bytes())
	if err != nil {
		return "", err
	}
	templateData["Imports"] = sdkImports

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "resource.go.tmpl", templateData); err != nil {
		return "", err
	}

	// The import list is already complete; FormatOnly keeps goimports from
	// running its own (ambiguous-package-prone) import resolution.
	formatted, err := imports.Process(convertedFile, buf.Bytes(), &imports.Options{
		Comments:   true,
		TabIndent:  true,
		TabWidth:   8,
		FormatOnly: true,
	})
	if err != nil {
		// Fall back to unformatted output if goimports fails — the file
		// still compiles since its imports were derived from usage.
		formatted = buf.Bytes()
	}

	if err := os.WriteFile(convertedFile, formatted, 0600); err != nil {
		return "", err
	}

	return convertedFile, nil
}

func dashboardv1Converter(resource *model.Resource) (string, error) {
	spec, err := resource.Spec()
	if err != nil {
		return "", err
	}

	marshalled, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}

	// Intentionally uses the v1 dashboard schema: convertersMap routes the
	// v0alpha1/v1/v1beta1 API versions here, so the deprecated type is the
	// correct one for these versions (dashboardv2 has an incompatible schema).
	object := dashboard.Dashboard{} //nolint:staticcheck // intentional v1 schema for v0alpha1/v1/v1beta1 imports
	if err = json.Unmarshal(marshalled, &object); err != nil {
		return "", err
	}

	return dashboard.DashboardConverter(object), nil
}

func dashboardv2Converter(resource *model.Resource) (string, error) {
	spec, err := resource.Spec()
	if err != nil {
		return "", err
	}

	marshalled, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}

	// Intentionally uses the v2beta1 dashboard schema: convertersMap routes the
	// v2beta1 API version here, so this is the schema type matching the imported
	// resource's version.
	object := dashboardv2beta1.Dashboard{} //nolint:staticcheck // intentional v2beta1 schema for v2beta1 imports
	if err = json.Unmarshal(marshalled, &object); err != nil {
		return "", err
	}

	return dashboardv2beta1.DashboardConverter(object), nil
}

func folderConverter(resource *model.Resource) (string, error) {
	spec, err := resource.Spec()
	if err != nil {
		return "", err
	}

	marshalled, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}

	// Intentionally uses the v1beta1 folder schema: convertersMap routes the
	// folder v1 API version here, matching the imported resource's version.
	object := folderv1beta1.Folder{} //nolint:staticcheck // intentional v1beta1 schema for folder imports
	if err = json.Unmarshal(marshalled, &object); err != nil {
		return "", err
	}

	return folderv1beta1.FolderConverter(object), nil
}
