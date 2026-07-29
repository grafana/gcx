//nolint:testpackage // tests need access to internal converters and the registry map
package dev

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	model "github.com/grafana/gcx/internal/resources"
	"github.com/grafana/grafana-foundation-sdk/go/cog/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestConvertersMapCoversReportedGVKs(t *testing.T) {
	required := []string{
		"Dashboard.dashboard.grafana.app/v0alpha1",
		"Dashboard.dashboard.grafana.app/v1",
		"Dashboard.dashboard.grafana.app/v1beta1",
		"Dashboard.dashboard.grafana.app/v2beta1",
		"Folder.folder.grafana.app/v1",
	}

	for _, key := range required {
		t.Run(key, func(t *testing.T) {
			_, ok := convertersMap[key]
			assert.True(t, ok, "convertersMap missing entry for %s", key)
		})
	}
}

func TestComputeSDKImports(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "sdk packages including nested variants",
			src: `package imported

import ()

func Example() {
	builder := dashboard.NewDashboardBuilder("t").
		Tooltip(common.TooltipDisplayModeSingle).
		Min(cog.ToPtr[float64](0)).
		WithTarget(prometheus.NewDataqueryBuilder().Expr("up"))
	var q []cog.Builder[variants.Dataquery]
	_, _ = builder, q
}
`,
			want: []string{
				"github.com/grafana/grafana-foundation-sdk/go/cog",
				"github.com/grafana/grafana-foundation-sdk/go/cog/variants",
				"github.com/grafana/grafana-foundation-sdk/go/common",
				"github.com/grafana/grafana-foundation-sdk/go/dashboard",
				"github.com/grafana/grafana-foundation-sdk/go/prometheus",
			},
		},
		{
			name: "locals and predeclared identifiers are not imports",
			src: `package imported

import ()

func Example() error {
	builder := resource.NewManifestBuilder()
	object, err := builder.Build()
	if err != nil {
		return err
	}
	_ = object.String()
	return nil
}
`,
			// builder/object/err resolve locally; only the package
			// reference `resource` remains.
			want: []string{"github.com/grafana/grafana-foundation-sdk/go/resource"},
		},
		{
			name: "selector-looking text inside string literals is ignored",
			src: `package imported

import ()

func Example() {
	_ = dashboard.NewDashboardBuilder("rate(http_requests_total{job=\"api\"}[5m]) and fake.Selector(x)")
}
`,
			want: []string{"github.com/grafana/grafana-foundation-sdk/go/dashboard"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := computeSDKImports([]byte(tt.src))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestComputeSDKImportsRejectsInvalidSource(t *testing.T) {
	_, err := computeSDKImports([]byte("not valid go"))
	require.Error(t, err)
}

// TestConvertResourceEmitsCompleteImports runs a realistic dashboard through
// convertResource and asserts the self-consistency property that motivated
// the usage-derived import block: every package referenced by a selector in
// the generated file is satisfied by its import list, without relying on
// goimports resolution (which cannot disambiguate e.g. "cog" candidates).
func TestConvertResourceEmitsCompleteImports(t *testing.T) {
	plugins.RegisterDefaultPlugins()

	res, err := model.FromUnstructured(&unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "dashboard.grafana.app/v1",
		"kind":       "Dashboard",
		"metadata":   map[string]any{"name": "import-test"},
		"spec": map[string]any{
			"title":         "Import Test",
			"schemaVersion": float64(39),
			"panels": []any{
				map[string]any{
					"id":    float64(1),
					"type":  "timeseries",
					"title": "Latency",
					"datasource": map[string]any{
						"type": "prometheus",
						"uid":  "prom",
					},
					"fieldConfig": map[string]any{
						"defaults": map[string]any{
							"unit": "s",
							"custom": map[string]any{
								"fillOpacity": float64(10),
								"lineWidth":   float64(2),
							},
						},
						"overrides": []any{},
					},
					"gridPos": map[string]any{"h": float64(8), "w": float64(12), "x": float64(0), "y": float64(0)},
					"targets": []any{
						map[string]any{
							"datasource": map[string]any{"type": "prometheus", "uid": "prom"},
							"expr":       `histogram_quantile(0.95, sum by (le) (rate(req_seconds_bucket[5m])))`,
							"refId":      "A",
						},
					},
				},
			},
		},
	}})
	require.NoError(t, err)

	dir := filepath.Join(t.TempDir(), "imported")
	writtenFile, err := convertResource(dir, res)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "import_test.go"), writtenFile)

	generated, err := os.ReadFile(filepath.Join(dir, "import_test.go"))
	require.NoError(t, err)

	// Sanity: this spec must exercise the ambiguous-package case that broke
	// goimports-based resolution. If the SDK converter stops emitting cog
	// references for it, pick a spec that does.
	require.Contains(t, string(generated), "cog.")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "import_test.go", generated, 0)
	require.NoError(t, err)

	imported := map[string]struct{}{}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		imported[filepath.Base(path)] = struct{}{}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		// Same resolution rule as computeSDKImports.
		if ident.Obj != nil || types.Universe.Lookup(ident.Name) != nil {
			return true
		}
		_, ok = imported[ident.Name]
		assert.True(t, ok, "generated file references package %q with no matching import", ident.Name)
		return true
	})
}

func TestConverters(t *testing.T) {
	tests := []struct {
		name         string
		object       map[string]any
		wantContains string
	}{
		{
			name: "dashboard v1",
			object: map[string]any{
				"apiVersion": "dashboard.grafana.app/v1",
				"kind":       "Dashboard",
				"metadata":   map[string]any{"name": "my-dashboard"},
				"spec": map[string]any{
					"title":         "My Dashboard",
					"schemaVersion": float64(36),
				},
			},
			wantContains: "NewDashboardBuilder",
		},
		{
			name: "folder v1",
			object: map[string]any{
				"apiVersion": "folder.grafana.app/v1",
				"kind":       "Folder",
				"metadata":   map[string]any{"name": "my-folder"},
				"spec": map[string]any{
					"title":       "My Folder",
					"description": "a folder",
				},
			},
			wantContains: "NewFolderBuilder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := model.FromUnstructured(&unstructured.Unstructured{Object: tt.object})
			require.NoError(t, err)

			gvk := res.GroupVersionKind()
			key := gvk.Kind + "." + gvk.GroupVersion().String()

			converter, ok := convertersMap[key]
			require.True(t, ok, "no converter registered for %s", key)

			code, err := converter(res)
			require.NoError(t, err)
			assert.Contains(t, code, tt.wantContains)
		})
	}
}
