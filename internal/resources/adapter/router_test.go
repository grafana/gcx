package adapter_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// -- descriptors for tests ---------------------------------------------------

//nolint:gochecknoglobals // Test fixture shared across test functions.
var sloGVK = schema.GroupVersionKind{
	Group:   "slo.ext.grafana.app",
	Version: "v1alpha1",
	Kind:    "SLO",
}

//nolint:gochecknoglobals // Test fixture shared across test functions.
var sloDescriptor = resources.Descriptor{
	GroupVersion: sloGVK.GroupVersion(),
	Kind:         "SLO",
	Singular:     "slo",
	Plural:       "slos",
}

//nolint:gochecknoglobals // Test fixture shared across test functions.
var dashboardGVK = schema.GroupVersionKind{
	Group:   "dashboard.grafana.app",
	Version: "v1beta1",
	Kind:    "Dashboard",
}

//nolint:gochecknoglobals // Test fixture shared across test functions.
var dashboardDescriptor = resources.Descriptor{
	GroupVersion: dashboardGVK.GroupVersion(),
	Kind:         "Dashboard",
	Singular:     "dashboard",
	Plural:       "dashboards",
}

// -- mock implementations ----------------------------------------------------

// mockDynamicClient records which operations were called.
type mockDynamicClient struct {
	listCalled        int
	getCalled         int
	getMultipleCalled int
	createCalled      int
	updateCalled      int
	deleteCalled      int

	listResult *unstructured.UnstructuredList
	getResult  *unstructured.Unstructured
	err        error
}

func (m *mockDynamicClient) List(_ context.Context, _ resources.Descriptor, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	m.listCalled++
	if m.listResult != nil {
		return m.listResult, m.err
	}
	return &unstructured.UnstructuredList{}, m.err
}

func (m *mockDynamicClient) Get(_ context.Context, _ resources.Descriptor, _ string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	m.getCalled++
	if m.getResult != nil {
		return m.getResult, m.err
	}
	return &unstructured.Unstructured{}, m.err
}

func (m *mockDynamicClient) GetMultiple(_ context.Context, _ resources.Descriptor, names []string, _ metav1.GetOptions) ([]unstructured.Unstructured, error) {
	m.getMultipleCalled++
	res := make([]unstructured.Unstructured, len(names))
	return res, m.err
}

func (m *mockDynamicClient) Create(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	m.createCalled++
	return obj, m.err
}

func (m *mockDynamicClient) Update(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	m.updateCalled++
	return obj, m.err
}

func (m *mockDynamicClient) Delete(_ context.Context, _ resources.Descriptor, _ string, _ metav1.DeleteOptions) error {
	m.deleteCalled++
	return m.err
}

// countingAdapter wraps mockAdapter and counts invocations.
// Counters are atomic because GetMultiple calls Get concurrently via errgroup.
type countingAdapter struct {
	mockAdapter

	listCalled   atomic.Int32
	getCalled    atomic.Int32
	createCalled atomic.Int32
	updateCalled atomic.Int32
	deleteCalled atomic.Int32
}

func (a *countingAdapter) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	a.listCalled.Add(1)
	return a.mockAdapter.List(ctx, opts)
}

func (a *countingAdapter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*unstructured.Unstructured, error) {
	a.getCalled.Add(1)
	return a.mockAdapter.Get(ctx, name, opts)
}

func (a *countingAdapter) Create(ctx context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions) (*unstructured.Unstructured, error) {
	a.createCalled.Add(1)
	return a.mockAdapter.Create(ctx, obj, opts)
}

func (a *countingAdapter) Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	a.updateCalled.Add(1)
	return a.mockAdapter.Update(ctx, obj, opts)
}

func (a *countingAdapter) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	a.deleteCalled.Add(1)
	return a.mockAdapter.Delete(ctx, name, opts)
}

// -- helpers -----------------------------------------------------------------

func newSLOAdapter() *countingAdapter {
	return &countingAdapter{
		mockAdapter: mockAdapter{desc: sloDescriptor, aliases: []string{"slo"}},
	}
}

// -- tests -------------------------------------------------------------------

// TestRouterDelegatesListToAdapter verifies:
// GIVEN a router with an SLO adapter registered
// WHEN List is called with an SLO descriptor
// THEN the SLO adapter's List is invoked.
func TestRouterDelegatesListToAdapter(t *testing.T) {
	sloAdapter := newSLOAdapter()
	dynClient := &mockDynamicClient{}

	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return sloAdapter, nil
	})

	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{
		sloGVK: factory,
	})

	_, err := router.List(context.Background(), sloDescriptor, metav1.ListOptions{})
	require.NoError(t, err)
	require.Equal(t, int32(1), sloAdapter.listCalled.Load(), "adapter List should be called once")
	require.Equal(t, 0, dynClient.listCalled, "dynamic client List should NOT be called")
}

// TestRouterDelegatesListToDynamicForNonProvider verifies:
// GIVEN a router with an SLO adapter registered
// WHEN List is called with a dashboard descriptor
// THEN the dynamic client's List is invoked.
func TestRouterDelegatesListToDynamicForNonProvider(t *testing.T) {
	sloAdapter := newSLOAdapter()
	dynClient := &mockDynamicClient{}

	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return sloAdapter, nil
	})

	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{
		sloGVK: factory,
	})

	_, err := router.List(context.Background(), dashboardDescriptor, metav1.ListOptions{})
	require.NoError(t, err)
	require.Equal(t, int32(0), sloAdapter.listCalled.Load(), "SLO adapter List should NOT be called")
	require.Equal(t, 1, dynClient.listCalled, "dynamic client List should be called once")
}

// TestRouterLazyInitDoesNotInvokeUnusedFactory verifies:
// GIVEN a router with a Synth adapter factory that panics
// WHEN List is called with a dashboard descriptor
// THEN the Synth factory is never invoked (lazy init).
func TestRouterLazyInitDoesNotInvokeUnusedFactory(t *testing.T) {
	panicFactory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		panic("factory should not be called for unrelated GVK")
	})
	dynClient := &mockDynamicClient{}

	synthGVK := schema.GroupVersionKind{
		Group:   "synthetic-monitoring.grafana.app",
		Version: "v1alpha1",
		Kind:    "Check",
	}

	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{
		synthGVK: panicFactory,
	})

	// This should not panic because the synth factory is never invoked.
	require.NotPanics(t, func() {
		_, err := router.List(context.Background(), dashboardDescriptor, metav1.ListOptions{})
		require.NoError(t, err)
	})
	require.Equal(t, 1, dynClient.listCalled, "dynamic client List should be called once")
}

// TestRouterCachesAdapterInstance verifies:
// GIVEN a router with an SLO adapter factory
// WHEN List is called twice for the same provider GVK
// THEN the adapter factory is invoked only once (caching).
func TestRouterCachesAdapterInstance(t *testing.T) {
	factoryCallCount := 0
	sloAdapter := newSLOAdapter()

	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		factoryCallCount++
		return sloAdapter, nil
	})

	dynClient := &mockDynamicClient{}
	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{
		sloGVK: factory,
	})

	_, err := router.List(context.Background(), sloDescriptor, metav1.ListOptions{})
	require.NoError(t, err)
	_, err = router.List(context.Background(), sloDescriptor, metav1.ListOptions{})
	require.NoError(t, err)

	require.Equal(t, 1, factoryCallCount, "factory should be called only once (caching)")
	require.Equal(t, int32(2), sloAdapter.listCalled.Load(), "adapter List should be called twice")
}

// TestRouterDelegatesGetToAdapter verifies Get routes to adapter when registered.
func TestRouterDelegatesGetToAdapter(t *testing.T) {
	sloAdapter := newSLOAdapter()
	dynClient := &mockDynamicClient{}

	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{
		sloGVK: func(_ context.Context) (adapter.ResourceAdapter, error) { return sloAdapter, nil },
	})

	_, err := router.Get(context.Background(), sloDescriptor, "my-slo", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, int32(1), sloAdapter.getCalled.Load())
	require.Equal(t, 0, dynClient.getCalled)
}

// TestRouterDelegatesGetToDynamicForNonProvider verifies Get falls back to dynamic client.
func TestRouterDelegatesGetToDynamicForNonProvider(t *testing.T) {
	dynClient := &mockDynamicClient{}
	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{})

	_, err := router.Get(context.Background(), dashboardDescriptor, "my-dash", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, dynClient.getCalled)
}

// TestRouterGetMultipleUsesErrGroupForAdapter verifies GetMultiple calls adapter.Get once per name
// (concurrently via errgroup) rather than delegating to the dynamic client's GetMultiple.
func TestRouterGetMultipleUsesErrGroupForAdapter(t *testing.T) {
	sloAdapter := newSLOAdapter()
	dynClient := &mockDynamicClient{}

	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{
		sloGVK: func(_ context.Context) (adapter.ResourceAdapter, error) { return sloAdapter, nil },
	})

	_, err := router.GetMultiple(context.Background(), sloDescriptor, []string{"slo-1", "slo-2", "slo-3"}, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, int32(3), sloAdapter.getCalled.Load(), "adapter Get should be called once per name")
	require.Equal(t, 0, dynClient.getMultipleCalled)
}

// TestRouterGetMultipleDelegatesToDynamicForNonProvider verifies GetMultiple falls back to dynamic.
func TestRouterGetMultipleDelegatesToDynamicForNonProvider(t *testing.T) {
	dynClient := &mockDynamicClient{}
	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{})

	_, err := router.GetMultiple(context.Background(), dashboardDescriptor, []string{"d1", "d2"}, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, dynClient.getMultipleCalled)
}

// TestRouterDelegatesCreateToAdapter verifies Create routes to adapter.
func TestRouterDelegatesCreateToAdapter(t *testing.T) {
	sloAdapter := newSLOAdapter()
	dynClient := &mockDynamicClient{}

	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{
		sloGVK: func(_ context.Context) (adapter.ResourceAdapter, error) { return sloAdapter, nil },
	})

	obj := &unstructured.Unstructured{}
	_, err := router.Create(context.Background(), sloDescriptor, obj, metav1.CreateOptions{})
	require.NoError(t, err)
	require.Equal(t, int32(1), sloAdapter.createCalled.Load())
	require.Equal(t, 0, dynClient.createCalled)
}

// TestRouterDelegatesUpdateToAdapter verifies Update routes to adapter.
func TestRouterDelegatesUpdateToAdapter(t *testing.T) {
	sloAdapter := newSLOAdapter()
	dynClient := &mockDynamicClient{}

	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{
		sloGVK: func(_ context.Context) (adapter.ResourceAdapter, error) { return sloAdapter, nil },
	})

	obj := &unstructured.Unstructured{}
	_, err := router.Update(context.Background(), sloDescriptor, obj, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Equal(t, int32(1), sloAdapter.updateCalled.Load())
	require.Equal(t, 0, dynClient.updateCalled)
}

// TestRouterDelegatesDeleteToAdapter verifies Delete routes to adapter.
func TestRouterDelegatesDeleteToAdapter(t *testing.T) {
	sloAdapter := newSLOAdapter()
	dynClient := &mockDynamicClient{}

	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{
		sloGVK: func(_ context.Context) (adapter.ResourceAdapter, error) { return sloAdapter, nil },
	})

	err := router.Delete(context.Background(), sloDescriptor, "my-slo", metav1.DeleteOptions{})
	require.NoError(t, err)
	require.Equal(t, int32(1), sloAdapter.deleteCalled.Load())
	require.Equal(t, 0, dynClient.deleteCalled)
}

// TestRouterDelegatesDeleteToDynamicForNonProvider verifies Delete falls back to dynamic.
func TestRouterDelegatesDeleteToDynamicForNonProvider(t *testing.T) {
	dynClient := &mockDynamicClient{}
	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{})

	err := router.Delete(context.Background(), dashboardDescriptor, "my-dash", metav1.DeleteOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, dynClient.deleteCalled)
}

// TestRouterFactoryErrorIsPropagated verifies factory errors are returned to caller.
func TestRouterFactoryErrorIsPropagated(t *testing.T) {
	factoryErr := errors.New("config not found")
	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return nil, factoryErr
	})

	dynClient := &mockDynamicClient{}
	router := adapter.NewResourceClientRouter(dynClient, map[schema.GroupVersionKind]adapter.Factory{
		sloGVK: factory,
	})

	_, err := router.List(context.Background(), sloDescriptor, metav1.ListOptions{})
	require.ErrorIs(t, err, factoryErr)
}

// hookAdapter supplies provider-owned behavior without exposing domain types to
// the router. The ordinary ResourceAdapter contract remains unchanged.
type hookAdapter struct {
	mockAdapter

	multipleItems []unstructured.Unstructured
	multipleErr   error
	selections    []adapter.PullSelection
	checkCalls    int
}

func (a *hookAdapter) GetMultiple(_ context.Context, _ []string, _ metav1.GetOptions) ([]unstructured.Unstructured, error) {
	return a.multipleItems, a.multipleErr
}
func (a *hookAdapter) NewPullPreflight(selections []adapter.PullSelection) adapter.PullPreflight {
	a.selections = selections
	return func(_ context.Context, _ resources.Filter, _ unstructured.Unstructured) error {
		a.checkCalls++
		return a.multipleErr
	}
}

func TestRouterPropagatesOptionalReadHooks(t *testing.T) {
	cause := errors.New("provider-owned read failure")
	item := unstructured.Unstructured{}
	item.SetGroupVersionKind(sloGVK)
	item.SetName("same")
	hooked := &hookAdapter{mockAdapter: mockAdapter{desc: sloDescriptor}, multipleItems: []unstructured.Unstructured{item}, multipleErr: cause}
	router := adapter.NewResourceClientRouter(&mockDynamicClient{}, map[schema.GroupVersionKind]adapter.Factory{
		sloGVK: func(context.Context) (adapter.ResourceAdapter, error) { return hooked, nil },
	})
	t.Run("multiple read retains provider items and error", func(t *testing.T) {
		items, err := router.GetMultiple(t.Context(), sloDescriptor, []string{"same", "missing"}, metav1.GetOptions{})
		require.ErrorIs(t, err, cause)
		require.Equal(t, hooked.multipleItems, items)
	})
	t.Run("preflight receives every batch and routes only its resource type", func(t *testing.T) {
		selections := []adapter.PullSelection{
			{Filter: resources.Filter{Descriptor: sloDescriptor}},
			{Filter: resources.Filter{Descriptor: sloDescriptor}, Items: []unstructured.Unstructured{item, item}},
			{Filter: resources.Filter{Descriptor: dashboardDescriptor}},
		}
		check, err := router.NewPullPreflight(t.Context(), selections)
		require.NoError(t, err)
		require.Equal(t, selections, hooked.selections)
		require.NoError(t, check(t.Context(), selections[2].Filter, item))
		require.Zero(t, hooked.checkCalls)
		require.ErrorIs(t, check(t.Context(), selections[1].Filter, item), cause)
		require.Equal(t, 1, hooked.checkCalls)
	})
}

func TestRouterPreflightSkipsEmptyFailedSelections(t *testing.T) {
	for _, failedRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "factory not loaded", true: "factory previously failed"}[failedRead], func(t *testing.T) {
			calls := 0
			cause := errors.New("adapter config unavailable")
			router := adapter.NewResourceClientRouter(&mockDynamicClient{}, map[schema.GroupVersionKind]adapter.Factory{
				sloGVK: func(context.Context) (adapter.ResourceAdapter, error) { calls++; return nil, cause },
			})
			if failedRead {
				_, err := router.List(t.Context(), sloDescriptor, metav1.ListOptions{})
				require.ErrorIs(t, err, cause)
			}
			item := unstructured.Unstructured{}
			item.SetGroupVersionKind(dashboardGVK)
			check, err := router.NewPullPreflight(t.Context(), []adapter.PullSelection{
				{Filter: resources.Filter{Descriptor: sloDescriptor}},
				{Filter: resources.Filter{Descriptor: dashboardDescriptor}, Items: []unstructured.Unstructured{item}},
			})
			require.NoError(t, err)
			require.NoError(t, check(t.Context(), resources.Filter{Descriptor: dashboardDescriptor}, item))
			if failedRead {
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, calls)
			}
		})
	}
}
