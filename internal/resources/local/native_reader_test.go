package local_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestFSReader_UnnamedNativeIdentities(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, tc := range []struct {
			name, kind, group string
			specs             []map[string]any
			apiNames          []string
			wantCount         int
		}{
			{
				name: "multiple intervals", kind: "TimeInterval",
				specs: []map[string]any{{"name": "weekdays"}, {"name": "weekends"}}, wantCount: 2,
			},
			{
				name: "multiple template groups", kind: "TemplateGroup",
				specs: []map[string]any{{"title": "weekday-template"}, {"title": "weekend-template"}}, wantCount: 2,
			},
			{
				name: "equal titles different template kinds", kind: "TemplateGroup",
				specs: []map[string]any{{"title": "same", "kind": "grafana"}, {"title": "same", "kind": "mimir"}}, wantCount: 2,
			},
			{
				name: "malformed intervals retained", kind: "TimeInterval",
				specs: []map[string]any{{"name": ""}, {"name": int64(42)}, {}}, wantCount: 3,
			},
			{
				name: "malformed template groups retained", kind: "TemplateGroup",
				specs: []map[string]any{{"title": ""}, {"title": "same", "kind": true}, {}}, wantCount: 3,
			},
			{
				name: "named native addresses remain authoritative", kind: "TimeInterval",
				specs: []map[string]any{{"name": "weekdays"}, {"name": "weekends"}}, apiNames: []string{"chosen", "chosen"}, wantCount: 1,
			},
			{
				name: "ordinary unnamed behavior unchanged", kind: "TimeInterval", group: "ordinary.grafana.app",
				specs: []map[string]any{{"name": "weekdays"}, {"name": "weekends"}}, wantCount: 1,
			},
		} {
			t.Run(version+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				group := tc.group
				if group == "" {
					group = "notifications.alerting.grafana.app"
				}
				for i, spec := range tc.specs {
					metadata := map[string]any{"namespace": "target"}
					if len(tc.apiNames) > 0 {
						metadata["name"] = tc.apiNames[i]
					}
					obj := &unstructured.Unstructured{Object: map[string]any{
						"apiVersion": group + "/" + version, "kind": tc.kind, "metadata": metadata, "spec": spec,
					}}
					filename := filepath.Join(dir, string(rune('a'+i))+".yaml")
					file, err := os.Create(filename)
					require.NoError(t, err)
					require.NoError(t, format.NewYAMLCodec().Encode(file, obj))
					require.NoError(t, file.Close())
				}
				reader := local.FSReader{Decoders: format.Codecs(), MaxConcurrentReads: 3, StopOnError: true}
				objects := resources.NewResources()
				require.NoError(t, reader.Read(t.Context(), objects, nil, []string{dir}))
				require.Equal(t, tc.wantCount, objects.Len())
				if tc.wantCount != len(tc.specs) {
					return
				}
				var gotSpecs, wantSpecs []string
				for _, obj := range objects.AsList() {
					data, err := json.Marshal(obj.Object.Object["spec"])
					require.NoError(t, err)
					gotSpecs = append(gotSpecs, string(data))
					require.Equal(t, "target", obj.Namespace())
					require.Empty(t, obj.Name())
				}
				for _, spec := range tc.specs {
					data, err := json.Marshal(spec)
					require.NoError(t, err)
					wantSpecs = append(wantSpecs, string(data))
				}
				require.ElementsMatch(t, wantSpecs, gotSpecs)
			})
		}
	}
}
