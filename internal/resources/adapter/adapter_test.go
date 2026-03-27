package adapter_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// mockAdapter is a minimal ResourceAdapter implementation for testing.
type mockAdapter struct {
	desc    resources.Descriptor
	aliases []string
}

var _ adapter.ResourceAdapter = &mockAdapter{}

func (m *mockAdapter) List(_ context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}

func (m *mockAdapter) Get(_ context.Context, _ string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	return &unstructured.Unstructured{}, nil
}

func (m *mockAdapter) Create(_ context.Context, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	return obj, nil
}

func (m *mockAdapter) Update(_ context.Context, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return obj, nil
}

func (m *mockAdapter) Delete(_ context.Context, _ string, _ metav1.DeleteOptions) error {
	return nil
}

func (m *mockAdapter) Descriptor() resources.Descriptor { return m.desc }
func (m *mockAdapter) Aliases() []string                { return m.aliases }
func (m *mockAdapter) Schema() json.RawMessage          { return nil }
func (m *mockAdapter) Example() json.RawMessage         { return nil }

func TestResourceAdapterInterfaceCompliance(t *testing.T) {
	desc := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "slo.ext.grafana.app", Version: "v1alpha1"},
		Kind:         "SLO",
		Singular:     "slo",
		Plural:       "slos",
	}

	a := &mockAdapter{
		desc:    desc,
		aliases: []string{"slo"},
	}

	t.Run("Descriptor returns registered descriptor", func(t *testing.T) {
		got := a.Descriptor()
		require.Equal(t, desc, got)
	})

	t.Run("Aliases returns registered aliases", func(t *testing.T) {
		got := a.Aliases()
		require.Equal(t, []string{"slo"}, got)
	})

	t.Run("List returns without error", func(t *testing.T) {
		result, err := a.List(context.Background(), metav1.ListOptions{})
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("Get returns without error", func(t *testing.T) {
		result, err := a.Get(context.Background(), "test", metav1.GetOptions{})
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("Create returns without error", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		result, err := a.Create(context.Background(), obj, metav1.CreateOptions{})
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("Update returns without error", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		result, err := a.Update(context.Background(), obj, metav1.UpdateOptions{})
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("Delete returns without error", func(t *testing.T) {
		err := a.Delete(context.Background(), "test", metav1.DeleteOptions{})
		require.NoError(t, err)
	})
}

func TestFactoryType(t *testing.T) {
	desc := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "slo.ext.grafana.app", Version: "v1alpha1"},
		Kind:         "SLO",
		Singular:     "slo",
		Plural:       "slos",
	}

	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return &mockAdapter{desc: desc, aliases: []string{"slo"}}, nil
	})

	a, err := factory(context.Background())
	require.NoError(t, err)
	require.Equal(t, "SLO", a.Descriptor().Kind)
}
