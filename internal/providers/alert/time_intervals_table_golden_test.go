package alert //nolint:testpackage // Exercises private command codec registrations.

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestTimeIntervalsTableGolden(t *testing.T) {
	testutils.SetAgentMode(t, false)
	protected := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": routingTreesGroup + "/v1beta1", "kind": timeIntervalKind,
		"metadata": map[string]any{"name": "opaque-weekends", "annotations": map[string]any{provenanceAnnotation: "system"}},
		"spec":     map[string]any{"name": "weekends", "time_intervals": []any{map[string]any{"weekdays": []any{"saturday"}}, map[string]any{"weekdays": []any{"sunday"}}}},
	}}
	editable := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": routingTreesGroup + "/v1beta1", "kind": timeIntervalKind,
		"metadata": map[string]any{"name": "opaque-holidays"}, "spec": map[string]any{"name": "holidays"},
	}}
	for _, name := range []string{"table", "wide"} {
		for _, tc := range []struct {
			name    string
			list    bool
			payload any
		}{
			{"list_rows", true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*protected, *editable}}},
			{"list_empty", true, &unstructured.UnstructuredList{}},
			{"get", false, protected},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				cmd := &cobra.Command{}
				opts := &timeIntervalsReadOpts{}
				opts.setup(cmd.Flags(), tc.list)
				require.NoError(t, cmd.Flags().Set("output", name))
				require.NoError(t, opts.Validate(cmd))
				var out bytes.Buffer
				require.NoError(t, opts.IO.Encode(&out, tc.payload))
				testutils.Golden(t, "time_intervals_"+name+"_"+tc.name, out.String())
			})
		}
	}
}
