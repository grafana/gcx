package kg_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScopeAdapter_List(t *testing.T) {
	crud := &adapter.TypedCRUD[kg.Scope]{
		Namespace:  "stack-1",
		Descriptor: kg.ScopeDescriptor(),
		ListFn: func(_ context.Context, _ int64) ([]kg.Scope, error) {
			return []kg.Scope{
				{Name: "env", Values: []string{"prod", "staging"}},
				{Name: "site", Values: []string{"us-east"}},
			}, nil
		},
	}

	a := crud.AsAdapter()
	result, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	item := result.Items[0]
	assert.Equal(t, "Scope", item.GetKind())
	assert.Equal(t, "env", item.GetName())
}

func TestRuleAdapter_List(t *testing.T) {
	crud := &adapter.TypedCRUD[kg.Rule]{
		Namespace:  "stack-1",
		Descriptor: kg.RuleDescriptor(),
		ListFn: func(_ context.Context, _ int64) ([]kg.Rule, error) {
			return []kg.Rule{
				{Name: "service:http_requests:rate5m", Expr: "sum(rate(http_requests_total[5m])) by (service)", Record: "service:http_requests:rate5m"},
				{Name: "high-error-rate", Alert: "HighErrorRate", Expr: "rate(http_errors_total[5m]) > 0.1"},
			}, nil
		},
	}

	a := crud.AsAdapter()
	result, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	item := result.Items[0]
	assert.Equal(t, "kg.ext.grafana.app/v1alpha1", item.GetAPIVersion())
	assert.Equal(t, "Rule", item.GetKind())
	assert.Equal(t, "service:http_requests:rate5m", item.GetName())
	assert.Equal(t, "stack-1", item.GetNamespace())
}

// TestKGProvider_TypedRegistrations verifies Rule and Scope resource types are registered.
func TestKGProvider_TypedRegistrations(t *testing.T) {
	p := &kg.KGProvider{}
	regs := p.TypedRegistrations()

	require.Len(t, regs, 2, "expected 2 registered resource types")

	wantKinds := map[string]bool{
		"Rule": false, "Scope": false,
	}
	for _, reg := range regs {
		kind := reg.Descriptor.Kind
		if _, ok := wantKinds[kind]; !ok {
			t.Errorf("unexpected kind %q in registrations", kind)
			continue
		}
		wantKinds[kind] = true

		assert.NotEmpty(t, reg.Descriptor.Singular, "kind %s missing singular", kind)
		assert.NotEmpty(t, reg.Descriptor.Plural, "kind %s missing plural", kind)
		assert.NotNil(t, reg.Factory, "kind %s missing factory", kind)
		assert.NotEmpty(t, reg.GVK.Kind, "kind %s missing GVK", kind)
		assert.NotNil(t, reg.Schema, "kind %s missing schema", kind)

		var m map[string]any
		require.NoError(t, json.Unmarshal(reg.Schema, &m), "kind %s schema is invalid JSON", kind)
	}

	for kind, found := range wantKinds {
		assert.True(t, found, "kind %q not found in registrations", kind)
	}
}
