// Package policies evaluates Grafana's CEL validation policies (policy.grafana.app) in-process,
// against local manifests or resources already stored in Grafana, without writing anything.
//
// Policies, bindings and their parameter objects are read from the Grafana instance, and policies
// are type-checked against the instance's OpenAPI schemas, so the outcome matches what admission
// would decide for the same objects.
package policies

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/grafana/grafana/pkg/policy/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Group is the API group of validation policies and their bindings.
const Group = "policy.grafana.app"

func policiesGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: Group, Version: "v0alpha1", Resource: "validationpolicies"}
}

func bindingsGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: Group, Version: "v0alpha1", Resource: "validationpolicybindings"}
}

// Policy is a validation policy as stored in Grafana, with the bindings that refer to it.
type Policy struct {
	api.Policy

	Bindings []api.Binding
}

// Load reads the namespace's validation policies and bindings. Bindings that refer to a policy
// that does not exist are dropped, as admission would ignore them.
func Load(ctx context.Context, client dynamic.Interface, namespace string) ([]Policy, error) {
	policyList, err := client.Resource(policiesGVR()).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing validation policies: %w", err)
	}
	bindingList, err := client.Resource(bindingsGVR()).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing validation policy bindings: %w", err)
	}

	byName := map[string]*Policy{}
	out := make([]Policy, 0, len(policyList.Items))
	for _, item := range policyList.Items {
		p, err := toPolicy(item)
		if err != nil {
			return nil, err
		}
		out = append(out, Policy{Policy: p})
	}
	for i := range out {
		byName[out[i].Name] = &out[i]
	}
	for _, item := range bindingList.Items {
		b, err := toBinding(item)
		if err != nil {
			return nil, err
		}
		if p, ok := byName[b.PolicyName]; ok {
			p.Bindings = append(p.Bindings, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// toPolicy converts a stored ValidationPolicy. The resource's spec uses the same field names as
// the engine's policy type, so it decodes directly; the resource name is the policy name.
func toPolicy(obj unstructured.Unstructured) (api.Policy, error) {
	var p api.Policy
	if err := decodeSpec(obj, &p); err != nil {
		return p, fmt.Errorf("decoding validation policy %q: %w", obj.GetName(), err)
	}
	p.Name = obj.GetName()
	return p, nil
}

// toBinding converts a stored ValidationPolicyBinding. A binding applies only to resources in
// its own namespace.
func toBinding(obj unstructured.Unstructured) (api.Binding, error) {
	var b api.Binding
	if err := decodeSpec(obj, &b); err != nil {
		return b, fmt.Errorf("decoding validation policy binding %q: %w", obj.GetName(), err)
	}
	b.Name = obj.GetName()
	b.Namespaces = []string{obj.GetNamespace()}
	return b, nil
}

func decodeSpec(obj unstructured.Unstructured, into any) error {
	spec, _, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil {
		return err
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}
