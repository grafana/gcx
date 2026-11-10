package versions

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestVersionsTableGolden(t *testing.T) {
	testutils.SetAgentMode(t, false)
	item := unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"generation": int64(4), "annotations": map[string]any{"grafana.app/updatedTimestamp": "2026-10-01T12:00:00Z", "grafana.app/updatedBy": "user:1", "grafana.app/message": "Update latency"}}}}
	for _, tc := range []struct {
		name  string
		items []unstructured.Unstructured
	}{{"rows", []unstructured.Unstructured{item, {Object: map[string]any{"metadata": map[string]any{"generation": int64(1)}}}}}, {"empty", nil}} {
		t.Run(tc.name, func(t *testing.T) {
			o := &versionsListOpts{}
			o.setup(pflag.NewFlagSet("versions", pflag.ContinueOnError))
			require.NoError(t, o.Validate())
			var buf bytes.Buffer
			require.NoError(t, o.IO.Encode(&buf, &unstructured.UnstructuredList{Items: tc.items}))
			testutils.Golden(t, "versions_table_"+tc.name, buf.String())
		})
	}
}
