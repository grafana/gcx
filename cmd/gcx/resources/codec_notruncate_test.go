package resources_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/resources"
	internalresources "github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// makeUnstructuredItem builds a single Unstructured resource with a full GVK.
func makeUnstructuredItem(group, version, kind, name string) unstructured.Unstructured {
	item := unstructured.Unstructured{}
	item.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
	item.SetName(name)
	item.SetCreationTimestamp(metav1.Time{})
	return item
}

func TestTableCodec_NoTruncate_StripsNewlines(t *testing.T) {
	tests := []struct {
		name         string
		noTruncate   bool
		resourceName string
		wantEllipsis bool
	}{
		{
			name:         "truncation active: newline in name produces ellipsis",
			noTruncate:   false,
			resourceName: "my-dashboard\nextra-line",
			wantEllipsis: true,
		},
		{
			name:         "no-truncate: newline in name replaced, no ellipsis",
			noTruncate:   true,
			resourceName: "my-dashboard\nextra-line",
			wantEllipsis: false,
		},
		{
			name:         "plain name: no ellipsis regardless of noTruncate",
			noTruncate:   false,
			resourceName: "my-dashboard",
			wantEllipsis: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			terminal.ResetForTesting()
			t.Cleanup(terminal.ResetForTesting)
			terminal.SetNoTruncate(tc.noTruncate)

			codec := &resources.TableCodecForTest{} // wide: false (zero value)
			item := makeUnstructuredItem("dashboard.grafana.app", "v1", "Dashboard", tc.resourceName)
			list := unstructured.UnstructuredList{Items: []unstructured.Unstructured{item}}

			var buf bytes.Buffer
			err := codec.Encode(&buf, list)
			require.NoError(t, err)

			output := buf.String()
			if tc.wantEllipsis {
				assert.Contains(t, output, "...", "expected ellipsis from newline truncation")
			} else {
				assert.NotContains(t, output, "...", "expected no ellipsis when no-truncate active")
			}
		})
	}
}

func TestTabCodec_NoTruncate_StripsNewlines(t *testing.T) {
	tests := []struct {
		name           string
		noTruncate     bool
		plural         string
		wantMidCellNL  bool
		wantSpaceValue bool
	}{
		{
			name:          "truncation off: newline in plural passes through",
			noTruncate:    false,
			plural:        "dash\nboards",
			wantMidCellNL: true,
		},
		{
			name:           "no-truncate: newline replaced with space",
			noTruncate:     true,
			plural:         "dash\nboards",
			wantMidCellNL:  false,
			wantSpaceValue: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			terminal.ResetForTesting()
			t.Cleanup(terminal.ResetForTesting)
			terminal.SetNoTruncate(tc.noTruncate)

			descs := internalresources.Descriptors{
				{
					GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1alpha1"},
					Plural:       tc.plural,
					Singular:     "dashboard",
					Kind:         "Dashboard",
				},
			}

			codec := &resources.TabCodecForTest{} // wide: false (zero value)
			var buf bytes.Buffer
			err := codec.Encode(&buf, descs)
			require.NoError(t, err)

			output := buf.String()
			hasMidCellNewline := strings.Contains(output, "dash\nboards")
			if tc.wantMidCellNL {
				assert.True(t, hasMidCellNewline, "expected mid-cell newline to be preserved")
			} else {
				assert.False(t, hasMidCellNewline, "expected mid-cell newline to be removed")
			}
			if tc.wantSpaceValue {
				assert.Contains(t, output, "dash boards", "expected space-replaced newline in output")
			}
		})
	}
}

func TestTableCodec_Columns(t *testing.T) {
	terminal.ResetForTesting()
	t.Cleanup(terminal.ResetForTesting)

	tests := []struct {
		name       string
		wide       bool
		items      []unstructured.Unstructured
		wantCols   []string
		unwantCols []string
		wantRows   [][]string // each inner slice is substrings expected on the same output line
	}{
		{
			name: "default columns include GROUP and NAME",
			wide: false,
			items: []unstructured.Unstructured{
				makeUnstructuredItem("dashboard.grafana.app", "v1", "Dashboard", "my-dash"),
			},
			wantCols:   []string{"KIND", "GROUP", "NAME"},
			unwantCols: []string{"NAMESPACE", "GROUPVERSION"},
			wantRows: [][]string{
				{"Dashboard", "dashboard.grafana.app", "my-dash"},
			},
		},
		{
			name: "wide columns include VERSION",
			wide: true,
			items: []unstructured.Unstructured{
				makeUnstructuredItem("dashboard.grafana.app", "v1", "Dashboard", "my-dash"),
			},
			wantCols:   []string{"KIND", "GROUP", "VERSION", "NAME"},
			unwantCols: []string{"NAMESPACE", "GROUPVERSION"},
			wantRows: [][]string{
				{"Dashboard", "dashboard.grafana.app", "v1", "my-dash"},
			},
		},
		{
			name: "multi-group resources show distinct groups",
			wide: false,
			items: []unstructured.Unstructured{
				makeUnstructuredItem("prometheus.datasource.grafana.app", "v0alpha1", "DataSource", "prom-ds"),
				makeUnstructuredItem("loki.datasource.grafana.app", "v0alpha1", "DataSource", "loki-ds"),
			},
			wantCols: []string{"KIND", "GROUP", "NAME"},
			wantRows: [][]string{
				{"DataSource", "prometheus.datasource.grafana.app", "prom-ds"},
				{"DataSource", "loki.datasource.grafana.app", "loki-ds"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := resources.NewTableCodecForTest(tc.wide)
			list := unstructured.UnstructuredList{Items: tc.items}

			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, list))
			output := buf.String()

			for _, col := range tc.wantCols {
				assert.Contains(t, output, col, "expected column header %q", col)
			}
			for _, col := range tc.unwantCols {
				assert.NotContains(t, output, col, "unexpected column header %q", col)
			}

			lines := strings.Split(output, "\n")
			for _, wantRow := range tc.wantRows {
				found := false
				for _, line := range lines {
					matchAll := true
					for _, sub := range wantRow {
						if !strings.Contains(line, sub) {
							matchAll = false
							break
						}
					}
					if matchAll {
						found = true
						break
					}
				}
				assert.True(t, found, "expected row containing %v in output:\n%s", wantRow, output)
			}
		})
	}
}
