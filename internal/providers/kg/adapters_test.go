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

func TestDatasetAdapter_List(t *testing.T) {
	crud := &adapter.TypedCRUD[kg.DatasetItem]{
		Namespace:  "stack-1",
		Descriptor: kg.DatasetDescriptor(),
		ListFn: func(_ context.Context, _ int64) ([]kg.DatasetItem, error) {
			return []kg.DatasetItem{
				{Name: "kubernetes", Detected: true, Enabled: true, Configured: true},
				{Name: "otel", Detected: true, Enabled: false, Configured: false},
			}, nil
		},
	}

	a := crud.AsAdapter()
	result, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	item := result.Items[0]
	assert.Equal(t, "kg.ext.grafana.app/v1alpha1", item.GetAPIVersion())
	assert.Equal(t, "Dataset", item.GetKind())
	assert.Equal(t, "kubernetes", item.GetName())
	assert.Equal(t, "stack-1", item.GetNamespace())
}

func TestDatasetAdapter_GetFallback(t *testing.T) {
	crud := &adapter.TypedCRUD[kg.DatasetItem]{
		Namespace:  "stack-1",
		Descriptor: kg.DatasetDescriptor(),
		ListFn: func(_ context.Context, _ int64) ([]kg.DatasetItem, error) {
			return []kg.DatasetItem{
				{Name: "kubernetes", Detected: true, Enabled: true},
				{Name: "otel", Detected: false, Enabled: false},
			}, nil
		},
		// GetFn nil — should fall back to list + filter
	}

	a := crud.AsAdapter()

	t.Run("finds by name", func(t *testing.T) {
		item, err := a.Get(t.Context(), "otel", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "otel", item.GetName())
	})

	t.Run("not found", func(t *testing.T) {
		_, err := a.Get(t.Context(), "missing", metav1.GetOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestVendorAdapter_List(t *testing.T) {
	crud := &adapter.TypedCRUD[kg.Vendor]{
		Namespace:  "stack-1",
		Descriptor: kg.VendorDescriptor(),
		ListFn: func(_ context.Context, _ int64) ([]kg.Vendor, error) {
			return []kg.Vendor{
				{Name: "nginx", Enabled: true},
				{Name: "redis", Enabled: false},
			}, nil
		},
	}

	a := crud.AsAdapter()
	result, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	item := result.Items[0]
	assert.Equal(t, "kg.ext.grafana.app/v1alpha1", item.GetAPIVersion())
	assert.Equal(t, "Vendor", item.GetKind())
	assert.Equal(t, "nginx", item.GetName())
}

func TestEntityTypeAdapter_List(t *testing.T) {
	crud := &adapter.TypedCRUD[kg.EntityType]{
		Namespace:  "stack-1",
		Descriptor: kg.EntityTypeDescriptor(),
		ListFn: func(_ context.Context, _ int64) ([]kg.EntityType, error) {
			return []kg.EntityType{
				{Name: "Service", Count: 42},
				{Name: "Namespace", Count: 5},
			}, nil
		},
	}

	a := crud.AsAdapter()
	result, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	item := result.Items[0]
	assert.Equal(t, "EntityType", item.GetKind())
	assert.Equal(t, "Service", item.GetName())

	spec, ok := item.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Service", spec["name"])
	assert.InDelta(t, float64(42), spec["count"], 0)
}

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

// TestKGProvider_TypedRegistrations verifies that all 5 resource types are registered.
func TestKGProvider_TypedRegistrations(t *testing.T) {
	p := &kg.KGProvider{}
	regs := p.TypedRegistrations()

	// Should have 5 registrations: Rule, Dataset, Vendor, EntityType, Scope
	require.Len(t, regs, 5, "expected 5 registered resource types")

	wantKinds := map[string]bool{
		"Rule": false, "Dataset": false, "Vendor": false,
		"EntityType": false, "Scope": false,
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

		// Verify schema is valid JSON
		var m map[string]any
		require.NoError(t, json.Unmarshal(reg.Schema, &m), "kind %s schema is invalid JSON", kind)
	}

	for kind, found := range wantKinds {
		assert.True(t, found, "kind %q not found in registrations", kind)
	}
}
