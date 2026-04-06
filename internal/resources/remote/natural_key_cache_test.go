package remote //nolint:testpackage // White-box test for internal cache mechanics.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type mockLister struct {
	mu        sync.Mutex
	callCount map[schema.GroupVersionKind]int
	results   map[schema.GroupVersionKind]*unstructured.UnstructuredList
	err       error
}

func newMockLister() *mockLister {
	return &mockLister{
		callCount: make(map[schema.GroupVersionKind]int),
		results:   make(map[schema.GroupVersionKind]*unstructured.UnstructuredList),
	}
}

func (m *mockLister) List(ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	gvk := desc.GroupVersionKind()
	m.callCount[gvk]++

	if m.err != nil {
		return nil, m.err
	}

	return m.results[gvk], nil
}

func (m *mockLister) calls(gvk schema.GroupVersionKind) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount[gvk]
}

func sloDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "slo.grafana.app", Version: "v1"},
		Kind:         "Slo",
		Singular:     "slo",
		Plural:       "slos",
	}
}

func dashboardDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
		Kind:         "Dashboard",
		Singular:     "dashboard",
		Plural:       "dashboards",
	}
}

func TestNaturalKeyCache_NilLister(t *testing.T) {
	cache := newNaturalKeyCache(nil)
	result, err := cache.list(context.Background(), sloDescriptor())
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestNaturalKeyCache_CacheMissCallsList(t *testing.T) {
	mock := newMockLister()
	expected := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			{Object: map[string]any{"metadata": map[string]any{"name": "test-slo"}}},
		},
	}
	mock.results[sloDescriptor().GroupVersionKind()] = expected

	cache := newNaturalKeyCache(mock)
	result, err := cache.list(context.Background(), sloDescriptor())

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	assert.Equal(t, 1, mock.calls(sloDescriptor().GroupVersionKind()))
}

func TestNaturalKeyCache_CacheHitSkipsList(t *testing.T) {
	mock := newMockLister()
	expected := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			{Object: map[string]any{"metadata": map[string]any{"name": "test-slo"}}},
		},
	}
	mock.results[sloDescriptor().GroupVersionKind()] = expected

	cache := newNaturalKeyCache(mock)

	// First call — cache miss.
	result1, err := cache.list(context.Background(), sloDescriptor())
	require.NoError(t, err)

	// Second call — cache hit.
	result2, err := cache.list(context.Background(), sloDescriptor())
	require.NoError(t, err)

	assert.Same(t, result1, result2)
	assert.Equal(t, 1, mock.calls(sloDescriptor().GroupVersionKind()))
}

func TestNaturalKeyCache_DifferentGVKsCachedIndependently(t *testing.T) {
	mock := newMockLister()
	sloList := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			{Object: map[string]any{"metadata": map[string]any{"name": "slo-1"}}},
		},
	}
	dashList := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			{Object: map[string]any{"metadata": map[string]any{"name": "dash-1"}}},
		},
	}
	mock.results[sloDescriptor().GroupVersionKind()] = sloList
	mock.results[dashboardDescriptor().GroupVersionKind()] = dashList

	cache := newNaturalKeyCache(mock)

	resultSlo, err := cache.list(context.Background(), sloDescriptor())
	require.NoError(t, err)
	assert.Equal(t, sloList, resultSlo)

	resultDash, err := cache.list(context.Background(), dashboardDescriptor())
	require.NoError(t, err)
	assert.Equal(t, dashList, resultDash)

	assert.Equal(t, 1, mock.calls(sloDescriptor().GroupVersionKind()))
	assert.Equal(t, 1, mock.calls(dashboardDescriptor().GroupVersionKind()))
}

func TestNaturalKeyCache_ListErrorPropagated(t *testing.T) {
	mock := newMockLister()
	mock.err = errors.New("connection refused")

	cache := newNaturalKeyCache(mock)
	result, err := cache.list(context.Background(), sloDescriptor())

	assert.Nil(t, result)
	assert.EqualError(t, err, "connection refused")
}
