package dev

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"text/template"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/resources"
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

//nolint:gochecknoglobals
var convertersMap = map[string]resourceConverter{
	"Dashboard.dashboard.grafana.app/v0alpha1": dashboardv1Converter,
	"Dashboard.dashboard.grafana.app/v1":       dashboardv1Converter,
	"Dashboard.dashboard.grafana.app/v1beta1":  dashboardv1Converter,
	"Dashboard.dashboard.grafana.app/v2beta1":  dashboardv2Converter,
	"Folder.folder.grafana.app/v1":             folderConverter,
}

type importOpts struct {
	Path string
}

func (opts *importOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&opts.Path, "path", "p", "imported", "Import path.")
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

			imported := 0
			err = res.Resources.ForEach(func(resource *model.Resource) error {
				if err := convertResource(opts.Path, resource); err != nil {
					resourceId := fmt.Sprintf("%s.%s", resource.Kind(), resource.Name())
					cmdio.Info(cmd.OutOrStdout(), "Skipping resource '%s': %s", resourceId, err)
					return nil
				}

				imported++
				return nil
			})
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Imported %d resources in %s", imported, opts.Path)

			return nil
		},
	}

	opts.setup(cmd.Flags())
	configOpts.BindFlags(cmd.Flags())

	return cmd
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

func convertResource(destinationRoot string, resource *model.Resource) error {
	tmpl, err := template.New("").Option("missingkey=error").ParseFS(templatesFS, "templates/import/*.tmpl")
	if err != nil {
		return err
	}

	gvk := resource.GroupVersionKind()
	converterKey := fmt.Sprintf("%s.%s", gvk.Kind, gvk.GroupVersion().String())

	converter, ok := convertersMap[converterKey]
	if !ok {
		return fmt.Errorf("no converter found for %s", converterKey)
	}

	converted, err := converter(resource)
	if err != nil {
		return err
	}

	convertedFile := filepath.Join(destinationRoot, strcase.ToSnakeCase(resource.Name())) + ".go"

	if err := ensureDirectory(filepath.Dir(convertedFile)); err != nil {
		return err
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
		return err
	}

	sdkImports, err := computeSDKImports(scanBuf.Bytes())
	if err != nil {
		return err
	}
	templateData["Imports"] = sdkImports

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "resource.go.tmpl", templateData); err != nil {
		return err
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

	return os.WriteFile(convertedFile, formatted, 0600)
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
