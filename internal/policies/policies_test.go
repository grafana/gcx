package policies_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/policies"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/grafana/pkg/policy/api"
	policyschema "github.com/grafana/grafana/pkg/policy/schema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

const namespace = "default"

func folderGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "folder.grafana.app", Version: "v1", Kind: "Folder"}
}

func namingGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "foldernaming.grafana.app", Version: "v0alpha1", Kind: "FolderNamingPolicy"}
}

func namingGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "foldernaming.grafana.app", Version: "v0alpha1", Resource: "foldernamingpolicies"}
}

func mustSchema(t *testing.T, raw string) *spec.Schema {
	t.Helper()
	s := &spec.Schema{}
	if err := json.Unmarshal([]byte(raw), s); err != nil {
		t.Fatal(err)
	}
	return s
}

func resolver(t *testing.T) policyschema.Resolver {
	t.Helper()
	return policyschema.StaticResolver{
		folderGVK(): mustSchema(t, `{"type":"object","properties":{"spec":{"type":"object","properties":{"title":{"type":"string"}}}}}`),
		namingGVK(): mustSchema(t, `{"type":"object","properties":{"spec":{"type":"object","properties":{"titlePattern":{"type":"string"}}}}}`),
	}
}

func obj(apiVersion, kind, name string, spec map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]any{"name": name, "namespace": namespace},
		"spec":       spec,
	}}
}

// namingPolicy mirrors what the folder naming app writes: the convention comes from the
// parameter object, and only creates and renames are checked.
func namingPolicy(name string) *unstructured.Unstructured {
	return obj("policy.grafana.app/v0alpha1", "ValidationPolicy", name, map[string]any{
		"match":           []any{map[string]any{"group": "folder.grafana.app", "versions": []any{"v1"}, "kinds": []any{"Folder"}}},
		"paramKind":       map[string]any{"group": namingGVK().Group, "version": namingGVK().Version, "kind": namingGVK().Kind},
		"matchConditions": []any{map[string]any{"name": "title-set", "expression": "oldObject == null || oldObject.spec.title != object.spec.title"}},
		"validations": []any{map[string]any{
			"name":              "title",
			"expression":        "object.spec.title.matches('^(?:' + params.spec.titlePattern + ')$')",
			"messageExpression": "'bad title ' + object.spec.title",
			"fieldPath":         "spec.title",
		}},
	})
}

func binding(name, policy, action, params string) *unstructured.Unstructured {
	return obj("policy.grafana.app/v0alpha1", "ValidationPolicyBinding", name, map[string]any{
		"policyName": policy,
		"actions":    []any{action},
		"paramRef":   map[string]any{"name": params},
	})
}

func newClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Group: policies.Group, Version: "v0alpha1", Resource: "validationpolicies"}:       "ValidationPolicyList",
		{Group: policies.Group, Version: "v0alpha1", Resource: "validationpolicybindings"}: "ValidationPolicyBindingList",
		namingGVR(): "FolderNamingPolicyList",
	}, objects...)
}

func resourceFor(gvk schema.GroupVersionKind) (schema.GroupVersionResource, bool) {
	if gvk == namingGVK() {
		return namingGVR(), true
	}
	return schema.GroupVersionResource{}, false
}

func folder(name, title string) *resources.Resource {
	return resources.MustFromObject(obj("folder.grafana.app/v1", "Folder", name, map[string]any{"title": title}).Object, resources.SourceInfo{})
}

func TestLoad(t *testing.T) {
	client := newClient(
		namingPolicy("team-prefix"),
		binding("team-prefix", "team-prefix", "Deny", "team-prefix"),
		binding("orphan", "missing-policy", "Deny", "team-prefix"),
	)
	got, err := policies.Load(t.Context(), client, namespace)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || got[0].Name != "team-prefix" {
		t.Fatalf("expected the team-prefix policy, got %+v", got)
	}
	if got[0].ParamKind == nil || got[0].ParamKind.Kind != "FolderNamingPolicy" {
		t.Fatalf("spec was not decoded: %+v", got[0].Policy)
	}
	if len(got[0].Bindings) != 1 {
		t.Fatalf("expected the orphan binding to be dropped, got %+v", got[0].Bindings)
	}
	b := got[0].Bindings[0]
	if b.PolicyName != "team-prefix" || b.ParamRef.Name != "team-prefix" || len(b.Namespaces) != 1 || b.Namespaces[0] != namespace {
		t.Fatalf("binding not converted: %+v", b)
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name         string
		objects      []runtime.Object
		resources    []*resources.Resource
		wantFindings []string // "<name>/<action>"
		wantDenied   int
		wantErrors   int
		wantChecked  int
	}{
		{
			name: "deny and warn bindings each report their violations",
			objects: []runtime.Object{
				namingPolicy("team-prefix"),
				binding("deny-prefix", "team-prefix", "Deny", "team-prefix"),
				binding("warn-short", "team-prefix", "Warn", "short"),
				obj("foldernaming.grafana.app/v0alpha1", "FolderNamingPolicy", "team-prefix", map[string]any{"titlePattern": "[a-z]+: .+"}),
				obj("foldernaming.grafana.app/v0alpha1", "FolderNamingPolicy", "short", map[string]any{"titlePattern": ".{1,10}"}),
			},
			resources: []*resources.Resource{
				folder("ok", "team: Ok"),
				folder("bad", "Bad title"),
				folder("long", "team: a long title"),
			},
			wantFindings: []string{"bad/Deny", "long/Warn"},
			wantDenied:   1,
			wantChecked:  3,
		},
		{
			name: "resources are evaluated as if created now",
			objects: []runtime.Object{
				namingPolicy("team-prefix"),
				binding("deny-prefix", "team-prefix", "Deny", "team-prefix"),
				obj("foldernaming.grafana.app/v0alpha1", "FolderNamingPolicy", "team-prefix", map[string]any{"titlePattern": "[a-z]+: .+"}),
			},
			// A stored folder that predates the convention breaks it, even though an update that
			// keeps its title would be admitted.
			resources:    []*resources.Resource{folder("legacy", "Legacy")},
			wantFindings: []string{"legacy/Deny"},
			wantDenied:   1,
			wantChecked:  1,
		},
		{
			name: "a binding whose parameter object is missing does not apply",
			objects: []runtime.Object{
				namingPolicy("team-prefix"),
				binding("deny-prefix", "team-prefix", "Deny", "deleted"),
			},
			resources:   []*resources.Resource{folder("bad", "Bad title")},
			wantChecked: 1,
		},
		{
			name: "a policy that does not compile is reported, not fatal",
			objects: []runtime.Object{
				obj("policy.grafana.app/v0alpha1", "ValidationPolicy", "broken", map[string]any{
					"match":       []any{map[string]any{"group": "folder.grafana.app", "versions": []any{"v1"}, "kinds": []any{"Folder"}}},
					"validations": []any{map[string]any{"expression": "object.spec.nope == 1"}},
				}),
			},
			resources:   []*resources.Resource{folder("any", "Anything")},
			wantErrors:  1,
			wantChecked: 1,
		},
		{
			name: "policy API resources are never evaluated",
			objects: []runtime.Object{
				namingPolicy("team-prefix"),
			},
			resources: []*resources.Resource{
				resources.MustFromObject(namingPolicy("other").Object, resources.SourceInfo{}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newClient(tt.objects...)
			loaded, err := policies.Load(t.Context(), client, namespace)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			evaluator, err := policies.NewEvaluator(namespace, loaded, resolver(t), client, resourceFor)
			if err != nil {
				t.Fatalf("NewEvaluator: %v", err)
			}
			report := evaluator.Evaluate(t.Context(), tt.resources)

			got := make([]string, 0, len(report.Findings))
			for _, f := range report.Findings {
				got = append(got, f.Name+"/"+string(f.Action))
				if f.Action == api.ActionDeny && f.FieldPath != "spec.title" {
					t.Errorf("finding lost its field path: %+v", f)
				}
			}
			if !equalSets(got, tt.wantFindings) {
				t.Fatalf("findings: got %v, want %v", got, tt.wantFindings)
			}
			if report.Denied() != tt.wantDenied {
				t.Fatalf("denied: got %d, want %d", report.Denied(), tt.wantDenied)
			}
			if len(report.PolicyErrors) != tt.wantErrors {
				t.Fatalf("policy errors: got %+v, want %d", report.PolicyErrors, tt.wantErrors)
			}
			if report.Checked != tt.wantChecked {
				t.Fatalf("checked: got %d, want %d", report.Checked, tt.wantChecked)
			}
		})
	}
}

func equalSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}
