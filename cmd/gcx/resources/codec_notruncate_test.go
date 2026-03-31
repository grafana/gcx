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

// makeUnstructuredList builds a minimal UnstructuredList with one item.
func makeUnstructuredList(name string) unstructured.UnstructuredList {
	item := unstructured.Unstructured{}
	item.SetKind("Dashboard")
	item.SetName(name)
	item.SetNamespace("default")
	item.SetCreationTimestamp(metav1.Time{})
	return unstructured.UnstructuredList{Items: []unstructured.Unstructured{item}}
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
			list := makeUnstructuredList(tc.resourceName)

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
