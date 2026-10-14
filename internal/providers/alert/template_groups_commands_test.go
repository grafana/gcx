package alert //nolint:testpackage // Tests inject bindings into private command factories.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type templateRun struct {
	stdout, stderr string
	err            error
}

func runTemplateGroups(t *testing.T, binding native.Binding, stdin string, args ...string) templateRun {
	t.Helper()
	var out, diag bytes.Buffer
	cmd := newTemplateGroupsCommand(binding)
	cmd.SetOut(&out)
	cmd.SetErr(&diag)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.ExecuteContext(t.Context())
	return templateRun{out.String(), diag.String(), err}
}
func templateTestMode(t *testing.T) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GCX_AUTO_APPROVE", "false")
}
func templateManifest(version, name, title, kind, rv string) string {
	spec := map[string]any{"title": title, "content": "{{ define \"test\" }}message{{ end }}", "unknownField": "preserve me"}
	if kind != "absent" {
		spec["kind"] = kind
	}
	obj := map[string]any{"apiVersion": routingTreesGroup + "/" + version, "kind": templateGroupKind, "spec": spec, "metadata": map[string]any{"name": name, "resourceVersion": rv, "namespace": "default", "uid": "source-uid", "generation": int64(8), "creationTimestamp": "2026-10-08T00:00:00Z", "managedFields": []any{map[string]any{"manager": "source"}}, "annotations": map[string]any{provenanceAnnotation: "api"}}}
	body, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return string(body)
}
func templateUsageCode(err error) int {
	if err == nil {
		return 0
	}
	d := fail.ErrorToDetailedError(err)
	if d != nil && d.ExitCode != nil {
		return *d.ExitCode
	}
	return gcxerrors.ExitGeneralError
}

func TestTemplateGroupsValidationBeforeLoad(t *testing.T) {
	templateTestMode(t)
	valid := templateManifest("v1beta1", "opaque", "shared", "grafana", "7")
	tests := []struct {
		name  string
		args  []string
		stdin string
	}{
		{"empty API version", []string{"list", "--api-version="}, ""},
		{"invalid version", []string{"list", "--api-version", "rules.alerting.grafana.app/v1"}, ""},
		{"empty get title", []string{"get", ""}, ""},
		{"empty get kind", []string{"get", "shared", "--kind="}, ""},
		{"invalid get kind", []string{"get", "shared", "--kind", "Grafana"}, ""},
		{"empty delete kind", []string{"delete", "shared", "--kind=", "--force"}, ""},
		{"empty filename", []string{"create", "-f", ""}, valid},
		{"blank filename", []string{"create", "-f", " "}, valid},
		{"wrong object kind", []string{"create", "-f", "-"}, strings.Replace(valid, `"kind":"TemplateGroup"`, `"kind":"TimeInterval"`, 1)},
		{"wrong group", []string{"create", "-f", "-"}, strings.Replace(valid, routingTreesGroup, "other.grafana.app", 1)},
		{"version conflict", []string{"create", "-f", "-", "--api-version", "v0alpha1"}, valid},
		{"empty title", []string{"create", "-f", "-"}, templateManifest("v1beta1", "", "", "grafana", "")},
		{"nonstring metadata name", []string{"update", "shared", "-f", "-"}, strings.Replace(valid, `"name":"opaque"`, `"name":42`, 1)},
		{"nonstring title", []string{"create", "-f", "-"}, strings.Replace(valid, `"title":"shared"`, `"title":42`, 1)},
		{"empty manifest kind", []string{"create", "-f", "-"}, templateManifest("v1beta1", "", "shared", "", "")},
		{"unknown manifest kind", []string{"create", "-f", "-"}, templateManifest("v1beta1", "", "shared", "unknown", "")},
		{"nonstring manifest kind", []string{"create", "-f", "-"}, strings.Replace(valid, `"kind":"grafana"`, `"kind":42`, 1)},
		{"missing update resource version", []string{"update", "shared", "-f", "-"}, templateManifest("v1beta1", "opaque", "shared", "grafana", "")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loads := 0
			binding := native.Func(func(context.Context, native.LoadOptions) (native.Access, error) {
				loads++
				return native.Access{}, errors.New("unexpected load")
			})
			res := runTemplateGroups(t, binding, tc.stdin, tc.args...)
			require.Error(t, res.err)
			assert.Equal(t, gcxerrors.ExitUsageError, templateUsageCode(res.err))
			assert.Zero(t, loads)
		})
	}
}
func TestTemplateGroupsReadOnlyValidationBeforeLoad(t *testing.T) {
	templateTestMode(t)
	for _, verb := range []string{"create", "update"} {
		t.Run(verb, func(t *testing.T) {
			loads := 0
			binding := native.Func(func(context.Context, native.LoadOptions) (native.Access, error) {
				loads++
				return native.Access{}, errors.New("unexpected load")
			})
			args := []string{verb}
			if verb == "update" {
				args = append(args, "shared")
			}
			args = append(args, "-f", "-")
			res := runTemplateGroups(t, binding, templateManifest("v1beta1", "imported", "shared", "mimir", "7"), args...)
			require.ErrorContains(t, res.err, "read-only")
			assert.Zero(t, loads)
		})
	}
}
func TestTemplateGroupsDeleteConfirmationBeforeLoad(t *testing.T) {
	templateTestMode(t)
	for _, agent := range []string{"false", "true"} {
		t.Run(agent, func(t *testing.T) {
			t.Setenv("GCX_AGENT_MODE", agent)
			loads := 0
			binding := native.Func(func(context.Context, native.LoadOptions) (native.Access, error) {
				loads++
				return native.Access{}, errors.New("unexpected load")
			})
			res := runTemplateGroups(t, binding, "", "delete", "shared")
			require.ErrorContains(t, res.err, "--force")
			assert.Zero(t, loads)
		})
	}
}
func TestTemplateGroupsVerbs(t *testing.T) {
	templateTestMode(t)
	cmd := newTemplateGroupsCommand(native.Fixed(native.Access{}))
	names := make([]string, 0, len(cmd.Commands()))
	for _, leaf := range cmd.Commands() {
		names = append(names, leaf.Name())
	}
	assert.ElementsMatch(t, []string{"list", "get", "create", "update", "delete"}, names)
}
func TestTemplateGroupsTable(t *testing.T) {
	templateTestMode(t)
	items := make([]unstructured.Unstructured, 0, 2)
	for _, data := range []string{templateManifest("v0alpha1", "old-api-name", "historical", "absent", "4"), templateManifest("v1beta1", "opaque-import", "shared", "mimir", "8")} {
		obj := unstructured.Unstructured{}
		require.NoError(t, obj.UnmarshalJSON([]byte(data)))
		items = append(items, obj)
	}
	for _, wide := range []bool{false, true} {
		t.Run(map[bool]string{false: "table", true: "wide"}[wide], func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, (&templateGroupTableCodec{wide: wide}).Encode(&out, &unstructured.UnstructuredList{Items: items}))
			for _, s := range []string{"TITLE", "API_NAME", "KIND", "READ-ONLY", "historical", "old-api-name", "grafana", "mimir", "true", "false"} {
				assert.Contains(t, out.String(), s)
			}
			if wide {
				assert.Contains(t, out.String(), "PROVENANCE")
				assert.Contains(t, out.String(), "api")
			}
		})
	}
	_, hasKind, _ := unstructured.NestedString(items[0].Object, "spec", "kind")
	assert.False(t, hasKind, "historical objects must not be defaulted")
}

func TestTemplateGroupsDeleteDeclined(t *testing.T) {
	templateTestMode(t)
	loads := 0
	binding := native.Func(func(context.Context, native.LoadOptions) (native.Access, error) {
		loads++
		return native.Access{}, errors.New("unexpected load")
	})
	res := runTemplateGroups(t, binding, "n\n", "delete", "shared")
	require.Error(t, res.err)
	assert.Equal(t, gcxerrors.ExitCancelled, templateUsageCode(res.err))
	assert.Contains(t, res.stderr, "Aborted.")
	assert.Empty(t, res.stdout)
	assert.Zero(t, loads)
}

func TestTemplateGroupsVersionSelection(t *testing.T) {
	templateTestMode(t)
	tests := []struct {
		args               []string
		stdin, wantVersion string
	}{
		{args: []string{"list"}, wantVersion: ""},
		{args: []string{"get", "shared", "--api-version", "v0alpha1"}, wantVersion: "v0alpha1"},
		{args: []string{"delete", "shared", "--force", "--api-version", routingTreesGroup + "/v1beta1"}, wantVersion: routingTreesGroup + "/v1beta1"},
		{args: []string{"create", "-f", "-"}, stdin: templateManifest("v0alpha1", "source", "new", "grafana", ""), wantVersion: "v0alpha1"},
		{args: []string{"update", "shared", "-f", "-", "--api-version", routingTreesGroup + "/v1beta1"}, stdin: templateManifest("v1beta1", "opaque", "shared", "grafana", "7"), wantVersion: "v1beta1"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var loads []native.LoadOptions
			stop := errors.New("stop after load")
			binding := native.Func(func(_ context.Context, opts native.LoadOptions) (native.Access, error) {
				loads = append(loads, opts)
				return native.Access{}, stop
			})
			res := runTemplateGroups(t, binding, tc.stdin, tc.args...)
			require.ErrorIs(t, res.err, stop)
			require.Len(t, loads, 1)
			assert.Equal(t, tc.wantVersion, loads[0].APIVersion)
		})
	}
}

func TestTemplateGroupsGetTableFormats(t *testing.T) {
	templateTestMode(t)
	for _, output := range []string{"table", "wide"} {
		for _, kind := range []string{"grafana", "mimir"} {
			t.Run(output+"/"+kind, func(t *testing.T) {
				api := newTemplateNativeAPI(t, "v1beta1")
				res := runTemplateGroups(t, templateAPIBinding(t, api), "", "get", "shared", "--kind", kind, "-o", output)
				require.NoError(t, res.err)
				for _, cell := range []string{"TITLE", "API_NAME", "KIND", "READ-ONLY", "shared", "opaque-" + kind} {
					assert.Contains(t, res.stdout, cell)
				}
				if output == "wide" {
					assert.Contains(t, res.stdout, "PROVENANCE")
				}
				require.Len(t, api.requests, 2)
				assert.Equal(t, "GET "+api.path, api.requests[0])
				assert.Equal(t, "GET "+api.path+"/opaque-"+kind, api.requests[1])
			})
		}
	}
}
