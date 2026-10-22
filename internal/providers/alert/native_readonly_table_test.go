package alert //nolint:testpackage // Exercises private native table codecs.

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNativeReadOnlyTables(t *testing.T) {
	for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
		t.Run(kind, func(t *testing.T) {
			spec := map[string]any{"name": "protected", "title": "protected", "kind": "grafana"}
			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "notifications.alerting.grafana.app/v1beta1", "kind": kind, "spec": spec,
				"metadata": map[string]any{"name": "opaque", "annotations": map[string]any{"grafana.com/provenance": "system"}},
			}}
			for _, wide := range []bool{false, true} {
				var out bytes.Buffer
				var err error
				if kind == "TimeInterval" {
					err = (&timeIntervalTableCodec{wide: wide}).Encode(&out, obj)
				} else {
					err = (&templateGroupTableCodec{wide: wide}).Encode(&out, obj)
				}
				require.NoError(t, err)
				require.Contains(t, out.String(), "READ-ONLY")
				require.Contains(t, out.String(), "true")
				if wide {
					require.Contains(t, out.String(), "system")
				}
			}
		})
	}
}

func TestTimeIntervalsReadOnlySubjectCannotMutate(t *testing.T) {
	for _, provenance := range []string{"system", "converted_prometheus", "file"} {
		for _, operation := range []string{"update", "delete"} {
			t.Run(provenance+"/"+operation, func(t *testing.T) {
				intervalEnv(t)
				current := intervalObject("protected", "opaque", "7")
				current.SetAnnotations(map[string]string{provenanceAnnotation: provenance})
				api := &intervalTestAPI{items: []unstructured.Unstructured{*current}}
				binding, _ := intervalBinding(t, api, "v1beta1")
				local := current.DeepCopy()
				local.SetAnnotations(map[string]string{provenanceAnnotation: "api"})
				args := []string{"delete", "protected", "--force"}
				if operation == "update" {
					args = []string{"update", "protected", "-f", "-"}
				}
				_, err := intervalRun(t, binding, intervalManifest(t, local), args...)
				require.ErrorContains(t, err, "read-only")
				require.ErrorContains(t, err, map[string]string{"system": "built into Grafana", "converted_prometheus": "external Alertmanager configuration", "file": "provisioning file"}[provenance])
				for _, request := range api.requests {
					require.Contains(t, request, "GET ")
				}
				require.Empty(t, api.bodies)
			})
		}
	}
}
