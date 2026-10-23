package dashboards

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDashboardTableGolden(t *testing.T) {
	testutils.SetAgentMode(t, false)
	item := unstructured.Unstructured{Object: map[string]any{"apiVersion": "dashboard.grafana.app/v1", "metadata": map[string]any{"name": "latency", "annotations": map[string]any{"grafana.app/folder": "services", "grafana.app/slug": "latency"}}, "spec": map[string]any{"title": "Latency", "tags": []any{"prod", "api"}, "panels": []any{map[string]any{}}}}}
	for _, name := range []string{"table", "wide"} {
		for _, tc := range []struct {
			name  string
			items []unstructured.Unstructured
		}{{"rows", []unstructured.Unstructured{item}}, {"empty", nil}} {
			t.Run(name+"_"+tc.name, func(t *testing.T) {
				o := &listOpts{}
				flags := pflag.NewFlagSet("list", pflag.ContinueOnError)
				o.setup(flags)
				require.NoError(t, flags.Set("output", name))
				require.NoError(t, o.Validate())
				if name == "wide" {
					o.IO.RegisterCustomCodec(name, dashboardListTable("https://example.grafana.net").Codec("wide"))
				}
				var buf bytes.Buffer
				require.NoError(t, o.IO.Encode(&buf, &unstructured.UnstructuredList{Items: tc.items}))
				testutils.Golden(t, "list_"+name+"_"+tc.name, buf.String())
			})
		}
		t.Run("get_"+name, func(t *testing.T) {
			o := &getOpts{}
			flags := pflag.NewFlagSet("get", pflag.ContinueOnError)
			o.setup(flags)
			require.NoError(t, flags.Set("output", name))
			require.NoError(t, o.Validate())
			var buf bytes.Buffer
			require.NoError(t, o.IO.Encode(&buf, &item))
			testutils.Golden(t, "get_"+name, buf.String())
		})
	}
}
