package alert //nolint:testpackage // Exercises private command codec registrations.

import (
	"bytes"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestTemplateGroupsTableGolden(t *testing.T) {
	testutils.SetAgentMode(t, false)
	items := make([]unstructured.Unstructured, 0, 5)
	for _, tc := range []struct{ title, kind, provenance string }{
		{"editable", "grafana", ""}, {"imported", "mimir", ""}, {"protected", "grafana", "system"}, {"invalid", "other", "file"}, {"default-kind", "", ""},
	} {
		spec := map[string]any{"title": tc.title}
		if tc.kind != "" {
			spec["kind"] = tc.kind
		}
		items = append(items, unstructured.Unstructured{Object: map[string]any{
			"apiVersion": routingTreesGroup + "/v1beta1", "kind": templateGroupKind,
			"metadata": map[string]any{"name": "opaque-" + tc.title, "annotations": map[string]any{provenanceAnnotation: tc.provenance}},
			"spec":     spec,
		}})
	}
	for _, name := range []string{"table", "wide"} {
		for _, tc := range []struct {
			name    string
			list    bool
			payload any
		}{
			{"list_rows", true, &unstructured.UnstructuredList{Items: items}},
			{"list_empty", true, &unstructured.UnstructuredList{}},
			{"get", false, &items[0]}, {"get_invalid", false, &items[3]},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				cmd := &cobra.Command{}
				var io *cmdio.Options
				if tc.list {
					opts := &templateGroupsListOpts{}
					opts.setup(cmd.Flags())
					io = &opts.IO
				} else {
					opts := &templateGroupsGetOpts{}
					opts.setup(cmd.Flags())
					io = &opts.IO
				}
				require.NoError(t, cmd.Flags().Set("output", name))
				require.NoError(t, io.Validate())
				var out bytes.Buffer
				require.NoError(t, io.Encode(&out, tc.payload))
				testutils.Golden(t, "template_groups_"+name+"_"+tc.name, out.String())
			})
		}
	}
}
