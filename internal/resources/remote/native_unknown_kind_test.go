package remote //nolint:testpackage // Tests exercise private identity matching before any mutation.

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNativeNaturalKeyUnknownServerKinds(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		desc := resources.Descriptor{GroupVersion: schema.GroupVersion{Group: "notifications.alerting.grafana.app", Version: version}, Kind: "TemplateGroup", Plural: "templategroups"}
		object := func(name, title, kind string) unstructured.Unstructured {
			return unstructured.Unstructured{Object: map[string]any{"apiVersion": desc.GroupVersion.String(), "kind": desc.Kind, "metadata": map[string]any{"name": name, "resourceVersion": "42"}, "spec": map[string]any{"title": title, "kind": kind}}}
		}
		for _, tc := range []struct {
			name, localKind, localTitle, errorText string
			duplicate, wantMatch                   bool
		}{
			{name: "unknown remote before valid match", localKind: "grafana", localTitle: "shared", wantMatch: true},
			{name: "unknown remote without valid match", localKind: "grafana", localTitle: "other"},
			{name: "valid matches remain ambiguous", localKind: "grafana", localTitle: "shared", duplicate: true, errorText: "ambiguous native identity"},
		} {
			t.Run(version+"/"+tc.name, func(t *testing.T) {
				lister := newMockLister()
				items := []unstructured.Unstructured{object("unknown-first", "shared", "future"), object("target", "shared", "grafana"), object("unknown-last", "shared", "future")}
				if tc.duplicate {
					items = append(items, object("duplicate", "shared", "grafana"))
				}
				lister.results[desc.GroupVersionKind()] = &unstructured.UnstructuredList{Items: items}
				local := object("", tc.localTitle, tc.localKind)
				original := local.DeepCopy()
				identity, _, err := resources.NativeIdentity(&local)
				require.NoError(t, err)
				matched, found, err := findByNaturalKey(t.Context(), newNaturalKeyCache(lister), desc, &local, identity)
				if tc.errorText != "" {
					require.ErrorContains(t, err, tc.errorText)
					assert.False(t, found)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tc.wantMatch, found)
					if found {
						assert.Equal(t, "target", matched.GetName())
						assert.Equal(t, "42", matched.GetResourceVersion())
					}
				}
				assert.Equal(t, original.Object, local.Object)
			})
		}
	}
}
