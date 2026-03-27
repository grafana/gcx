package adapter_test

// Integration tests for the full pipeline: registry -> adapter registration -> router.
//
// Unlike router_test.go (which tests the router in isolation), these tests verify
// the COMBINATION of:
//   - A discovery.Registry with RegisterAdapter called (static provider descriptor)
//   - A ResourceClientRouter built from the registry's adapter factories
//   - Correct routing: provider GVKs go to the adapter, native GVKs go to dynamic client
//
// The tests use a mock adapter and a mock dynamic client that panics for provider GVKs,
// ensuring that if routing is wrong the test fails loudly.

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// -- shared descriptors ------------------------------------------------------

//nolint:gochecknoglobals // Test fixtures — same pattern as router_test.go.
var integrationSLODescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{Group: "slo.ext.grafana.app", Version: "v1alpha1"},
	Kind:         "SLO",
	Singular:     "slo",
	Plural:       "slos",
}

//nolint:gochecknoglobals // Test fixtures — derived from integrationSLODescriptor.
var integrationSLOGVK = integrationSLODescriptor.GroupVersionKind()

//nolint:gochecknoglobals // Test fixtures — same pattern as router_test.go.
var integrationDashboardDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"},
	Kind:         "Dashboard",
	Singular:     "dashboard",
	Plural:       "dashboards",
}

// -- panicDynamicClient panics when provider GVKs are used -------------------
//
// This ensures that if the router incorrectly routes a provider GVK to the
// dynamic client (instead of the adapter), the test fails loudly.

type panicDynamicClient struct {
	// allowedGVKs is the set of GVKs the client is allowed to serve.
	// Calls for any other GVK will panic.
	allowedGVKs map[schema.GroupVersionKind]struct{}

	// track calls for allowed GVKs
	listCalled   int
	getCalled    int
	createCalled int
	updateCalled int
	deleteCalled int
}

func newPanicDynamicClient(allowed ...resources.Descriptor) *panicDynamicClient {
	m := make(map[schema.GroupVersionKind]struct{}, len(allowed))
	for _, d := range allowed {
		m[d.GroupVersionKind()] = struct{}{}
	}
	return &panicDynamicClient{allowedGVKs: m}
}

func (p *panicDynamicClient) checkAllowed(desc resources.Descriptor) {
	if _, ok := p.allowedGVKs[desc.GroupVersionKind()]; !ok {
		panic("dynamic client called for provider GVK (should be routed to adapter): " + desc.GroupVersionKind().String())
	}
}

func (p *panicDynamicClient) List(_ context.Context, desc resources.Descriptor, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	p.checkAllowed(desc)
	p.listCalled++
	return &unstructured.UnstructuredList{}, nil
}

func (p *panicDynamicClient) Get(_ context.Context, desc resources.Descriptor, _ string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	p.checkAllowed(desc)
	p.getCalled++
	return &unstructured.Unstructured{}, nil
}

func (p *panicDynamicClient) GetMultiple(_ context.Context, desc resources.Descriptor, names []string, _ metav1.GetOptions) ([]unstructured.Unstructured, error) {
	p.checkAllowed(desc)
	p.getCalled++
	return make([]unstructured.Unstructured, len(names)), nil
}

func (p *panicDynamicClient) Create(_ context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	p.checkAllowed(desc)
	p.createCalled++
	return obj, nil
}

func (p *panicDynamicClient) Update(_ context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	p.checkAllowed(desc)
	p.updateCalled++
	return obj, nil
}

func (p *panicDynamicClient) Delete(_ context.Context, desc resources.Descriptor, _ string, _ metav1.DeleteOptions) error {
	p.checkAllowed(desc)
	p.deleteCalled++
	return nil
}

// -- helpers -----------------------------------------------------------------

// buildPipelineFromStaticRegistry is the canonical helper: registers a factory in a
// static registry and returns a router wired to that registry.
// Unlike buildRouterFromRegistry it derives the GVK from the descriptor directly.
func buildPipelineFromStaticRegistry(
	factory adapter.Factory,
	desc resources.Descriptor,
	aliases []string,
	dynClient adapter.DynamicClient,
) *adapter.ResourceClientRouter {
	reg := discovery.NewStaticRegistry()
	reg.RegisterAdapter(factory, desc, aliases)

	factories := map[schema.GroupVersionKind]adapter.Factory{}
	if f, ok := reg.GetAdapter(desc.GroupVersionKind()); ok {
		factories[desc.GroupVersionKind()] = f
	}

	return adapter.NewResourceClientRouter(dynClient, factories)
}

// -- tests -------------------------------------------------------------------

// TestIntegrationListProviderResourceRoutesToAdapter verifies the full pipeline:
// GIVEN a static registry with an SLO adapter registered via RegisterAdapter
// WHEN a router is built from the registry and List is called with an SLO descriptor
// THEN the SLO adapter handles the call (not the dynamic client).
func TestIntegrationListProviderResourceRoutesToAdapter(t *testing.T) {
	sloAdapter := &countingAdapter{
		mockAdapter: mockAdapter{desc: integrationSLODescriptor, aliases: []string{"slo"}},
	}
	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return sloAdapter, nil
	})
	// The dynamic client panics if it receives an SLO GVK — only dashboard GVKs are allowed.
	dynClient := newPanicDynamicClient(integrationDashboardDescriptor)

	router := buildPipelineFromStaticRegistry(factory, integrationSLODescriptor, []string{"slo"}, dynClient)

	require.NotPanics(t, func() {
		_, err := router.List(context.Background(), integrationSLODescriptor, metav1.ListOptions{})
		require.NoError(t, err)
	})
	require.Equal(t, 1, sloAdapter.listCalled, "SLO adapter should handle List")
	require.Equal(t, 0, dynClient.listCalled, "dynamic client should NOT be called for SLO")
}

// TestIntegrationNativeResourceFallsToDynamicClient verifies:
// GIVEN a registry with an SLO adapter registered
// WHEN a router built from the registry receives a Dashboard List call
// THEN the dynamic client handles it (adapter is not called).
func TestIntegrationNativeResourceFallsToDynamicClient(t *testing.T) {
	sloAdapter := &countingAdapter{
		mockAdapter: mockAdapter{desc: integrationSLODescriptor, aliases: []string{"slo"}},
	}
	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return sloAdapter, nil
	})
	dynClient := &mockDynamicClient{} // plain mock, no panics

	router := buildPipelineFromStaticRegistry(factory, integrationSLODescriptor, []string{"slo"}, dynClient)

	_, err := router.List(context.Background(), integrationDashboardDescriptor, metav1.ListOptions{})
	require.NoError(t, err)
	require.Equal(t, 0, sloAdapter.listCalled, "SLO adapter should NOT be called for dashboards")
	require.Equal(t, 1, dynClient.listCalled, "dynamic client should handle dashboards")
}

// TestIntegrationGetProviderResourceRoutesToAdapter verifies:
// GIVEN a registry with an SLO adapter registered
// WHEN Get is called with an SLO descriptor
// THEN the SLO adapter handles the call.
func TestIntegrationGetProviderResourceRoutesToAdapter(t *testing.T) {
	sloAdapter := &countingAdapter{
		mockAdapter: mockAdapter{desc: integrationSLODescriptor, aliases: []string{"slo"}},
	}
	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return sloAdapter, nil
	})
	dynClient := newPanicDynamicClient(integrationDashboardDescriptor)

	router := buildPipelineFromStaticRegistry(factory, integrationSLODescriptor, []string{"slo"}, dynClient)

	require.NotPanics(t, func() {
		_, err := router.Get(context.Background(), integrationSLODescriptor, "my-slo", metav1.GetOptions{})
		require.NoError(t, err)
	})
	require.Equal(t, 1, sloAdapter.getCalled)
}

// TestIntegrationCreateProviderResourceRoutesToAdapter verifies:
// GIVEN a registry with an SLO adapter registered
// WHEN Create is called with an SLO descriptor
// THEN the SLO adapter handles the call (not the dynamic client).
func TestIntegrationCreateProviderResourceRoutesToAdapter(t *testing.T) {
	sloAdapter := &countingAdapter{
		mockAdapter: mockAdapter{desc: integrationSLODescriptor, aliases: []string{"slo"}},
	}
	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return sloAdapter, nil
	})
	dynClient := newPanicDynamicClient(integrationDashboardDescriptor)

	router := buildPipelineFromStaticRegistry(factory, integrationSLODescriptor, []string{"slo"}, dynClient)

	obj := &unstructured.Unstructured{}
	require.NotPanics(t, func() {
		_, err := router.Create(context.Background(), integrationSLODescriptor, obj, metav1.CreateOptions{})
		require.NoError(t, err)
	})
	require.Equal(t, 1, sloAdapter.createCalled)
	require.Equal(t, 0, dynClient.createCalled)
}

// TestIntegrationUpdateProviderResourceRoutesToAdapter verifies:
// GIVEN a registry with an SLO adapter registered
// WHEN Update is called with an SLO descriptor
// THEN the SLO adapter handles the call (not the dynamic client).
func TestIntegrationUpdateProviderResourceRoutesToAdapter(t *testing.T) {
	sloAdapter := &countingAdapter{
		mockAdapter: mockAdapter{desc: integrationSLODescriptor, aliases: []string{"slo"}},
	}
	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return sloAdapter, nil
	})
	dynClient := newPanicDynamicClient(integrationDashboardDescriptor)

	router := buildPipelineFromStaticRegistry(factory, integrationSLODescriptor, []string{"slo"}, dynClient)

	obj := &unstructured.Unstructured{}
	require.NotPanics(t, func() {
		_, err := router.Update(context.Background(), integrationSLODescriptor, obj, metav1.UpdateOptions{})
		require.NoError(t, err)
	})
	require.Equal(t, 1, sloAdapter.updateCalled)
	require.Equal(t, 0, dynClient.updateCalled)
}

// TestIntegrationDeleteProviderResourceRoutesToAdapter verifies:
// GIVEN a registry with an SLO adapter registered
// WHEN Delete is called with an SLO descriptor
// THEN the SLO adapter handles the call (not the dynamic client).
func TestIntegrationDeleteProviderResourceRoutesToAdapter(t *testing.T) {
	sloAdapter := &countingAdapter{
		mockAdapter: mockAdapter{desc: integrationSLODescriptor, aliases: []string{"slo"}},
	}
	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return sloAdapter, nil
	})
	dynClient := newPanicDynamicClient(integrationDashboardDescriptor)

	router := buildPipelineFromStaticRegistry(factory, integrationSLODescriptor, []string{"slo"}, dynClient)

	require.NotPanics(t, func() {
		err := router.Delete(context.Background(), integrationSLODescriptor, "my-slo", metav1.DeleteOptions{})
		require.NoError(t, err)
	})
	require.Equal(t, 1, sloAdapter.deleteCalled)
	require.Equal(t, 0, dynClient.deleteCalled)
}

// TestIntegrationLazyInitDoesNotLoadUnusedProviders verifies:
// GIVEN a registry with an SLO adapter registered (whose factory panics)
// WHEN a Dashboard List is executed (not an SLO operation)
// THEN the SLO factory is never invoked (lazy init: provider config not loaded eagerly).
func TestIntegrationLazyInitDoesNotLoadUnusedProviders(t *testing.T) {
	panicFactory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		panic("SLO factory should not be called for unrelated GVK")
	})
	dynClient := &mockDynamicClient{}

	router := buildPipelineFromStaticRegistry(panicFactory, integrationSLODescriptor, []string{"slo"}, dynClient)

	require.NotPanics(t, func() {
		_, err := router.List(context.Background(), integrationDashboardDescriptor, metav1.ListOptions{})
		require.NoError(t, err)
	})
	require.Equal(t, 1, dynClient.listCalled, "dynamic client should handle dashboard List")
}

// TestIntegrationAdapterIsRegisteredInRegistryIndex verifies:
// GIVEN an adapter registered via RegisterAdapter with aliases
// WHEN the registry index is checked (HasAdapter, GetAdapter)
// THEN the GVK is recognized as adapter-backed.
func TestIntegrationAdapterIsRegisteredInRegistryIndex(t *testing.T) {
	reg := discovery.NewStaticRegistry()
	factory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return &mockAdapter{desc: integrationSLODescriptor, aliases: []string{"slo"}}, nil
	})
	reg.RegisterAdapter(factory, integrationSLODescriptor, []string{"slo"})

	t.Run("HasAdapter returns true for registered GVK", func(t *testing.T) {
		require.True(t, reg.HasAdapter(integrationSLOGVK))
	})

	t.Run("GetAdapter returns factory for registered GVK", func(t *testing.T) {
		f, ok := reg.GetAdapter(integrationSLOGVK)
		require.True(t, ok)
		require.NotNil(t, f)

		// Verify the factory returns a valid adapter.
		a, err := f(context.Background())
		require.NoError(t, err)
		require.Equal(t, "SLO", a.Descriptor().Kind)
	})

	t.Run("HasAdapter returns false for unregistered GVK", func(t *testing.T) {
		require.False(t, reg.HasAdapter(integrationDashboardDescriptor.GroupVersionKind()))
	})
}

// TestIntegrationNewProviderCanParticipateByImplementingInterface verifies:
// GIVEN the ResourceAdapter interface exists
// WHEN a new provider implements ResourceAdapter and registers a static descriptor
// THEN it can participate in the resources pipeline without modifying the resources command code.
//
// This test directly encodes the spec acceptance criterion:
// "GIVEN the ResourceAdapter interface exists WHEN a new provider is implemented
// THEN it can participate in the resources pipeline by implementing ResourceAdapter
// and registering a static descriptor, without modifying the resources command code".
func TestIntegrationNewProviderCanParticipateByImplementingInterface(t *testing.T) {
	// Simulate a new provider "checks" registering its adapter.
	checksGVK := schema.GroupVersionKind{
		Group:   "synthetic-monitoring.grafana.app",
		Version: "v1alpha1",
		Kind:    "Check",
	}
	checksDescriptor := resources.Descriptor{
		GroupVersion: checksGVK.GroupVersion(),
		Kind:         "Check",
		Singular:     "check",
		Plural:       "checks",
	}

	// The new provider implements ResourceAdapter (compile-time check below).
	checksFactory := adapter.Factory(func(_ context.Context) (adapter.ResourceAdapter, error) {
		return &mockAdapter{desc: checksDescriptor, aliases: []string{"checks"}}, nil
	})

	// Register via static registry (no resources command code modification needed).
	reg := discovery.NewStaticRegistry()
	reg.RegisterAdapter(checksFactory, checksDescriptor, []string{"checks"})

	// Verify the new type is recognized.
	require.True(t, reg.HasAdapter(checksGVK), "new provider type should be recognized by registry")

	// Build the router and verify routing works.
	f, ok := reg.GetAdapter(checksGVK)
	require.True(t, ok)
	router := adapter.NewResourceClientRouter(&mockDynamicClient{}, map[schema.GroupVersionKind]adapter.Factory{
		checksGVK: f,
	})

	// List call goes to the new provider's adapter.
	result, err := router.List(context.Background(), checksDescriptor, metav1.ListOptions{})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Compile-time: mockAdapter (used by the factory above) satisfies ResourceAdapter.
	var _ adapter.ResourceAdapter = &mockAdapter{}
}
