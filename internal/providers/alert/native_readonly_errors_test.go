package alert //nolint:testpackage // Drive the native command error boundary with injected bindings.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestNativeReadOnlyManifestExitCode(t *testing.T) {
	for _, kind := range []string{timeIntervalKind, templateGroupKind} {
		for _, source := range []string{"system", "converted_prometheus", "file", "mimir"} {
			if kind == timeIntervalKind && source == "mimir" {
				continue
			}
			for _, verb := range []string{"create", "update"} {
				t.Run(kind+"/"+source+"/"+verb, func(t *testing.T) {
					templateTestMode(t)
					obj := intervalObject("protected", "opaque", "7")
					if kind == templateGroupKind {
						obj.SetKind(templateGroupKind)
						obj.Object["spec"] = map[string]any{"title": "protected", "kind": "grafana", "content": "{{ define \"protected\" }}value{{ end }}"}
					}
					if source == "mimir" {
						obj.Object["spec"] = map[string]any{"title": "protected", "kind": "mimir"}
					} else {
						obj.SetAnnotations(map[string]string{provenanceAnnotation: source})
					}
					data, err := json.Marshal(obj)
					require.NoError(t, err)
					loads := 0
					binding := native.Func(func(context.Context, native.LoadOptions) (native.Access, error) {
						loads++
						return native.Access{}, errors.New("unexpected load")
					})
					args := []string{verb}
					if verb == "update" {
						args = append(args, "protected")
					}
					args = append(args, "-f", "-")
					if kind == templateGroupKind {
						err = runTemplateGroups(t, binding, string(data), args...).err
					} else {
						_, err = intervalRun(t, binding, string(data), args...)
					}
					require.ErrorContains(t, err, "read-only")
					require.Equal(t, gcxerrors.ExitGeneralError, templateUsageCode(err))
					require.NotContains(t, err.Error(), "--help")
					require.Zero(t, loads)
				})
			}
		}
	}
}

func TestTemplateGroupsUpdateDoesNotSelectImportedGroup(t *testing.T) {
	templateTestMode(t)
	api := newTemplateNativeAPI(t, "v1beta1")
	var imported map[string]any
	require.NoError(t, json.Unmarshal([]byte(templateManifest("v1beta1", "imported", "shared", "mimir", "7")), &imported))
	api.items = map[string]map[string]any{"imported": imported}
	binding := templateAPIBinding(t, api)
	result := runTemplateGroups(t, binding, templateManifest("v1beta1", "", "shared", "grafana", "7"), "update", "shared", "-f", "-")
	require.True(t, apierrors.IsNotFound(result.err), "a Grafana-managed update must not select an imported group")
	require.Empty(t, api.body)
}
