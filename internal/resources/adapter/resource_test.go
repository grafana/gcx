package adapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// fakeGadget is a small self-contained domain type used to exercise the
// declarative Resource[T]/NewProvider capability seam, independent of any
// real provider (SLO, OnCall, ...).
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type fakeGadget struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

func (g fakeGadget) GetResourceName() string      { return g.Name }
func (g *fakeGadget) SetResourceName(name string) { g.Name = name }

// fakeDeps is a stub DepsLoader for tests that don't care about real config.
func fakeDeps(context.Context) (adapter.ClientDeps, error) {
	return adapter.ClientDeps{Namespace: "stack-1"}, nil
}

// buildGadgetUnstructured builds a minimal unstructured envelope matching
// fakeGadget's spec shape, for driving ResourceAdapter.Create/Update.
func buildGadgetUnstructured(name, value string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "test.grafana.app/v1alpha1",
			"kind":       "Gadget",
			"metadata":   map[string]any{"name": name},
			"spec":       map[string]any{"name": name, "value": value},
		},
	}
}

// --- AC-003: read-only client resolves the missing verbs to ErrUnsupported ---

// readOnlyGadgetClient implements only Lister[fakeGadget] and
// Getter[fakeGadget] — no Creator/Updater/Deleter.
type readOnlyGadgetClient struct {
	items []fakeGadget
}

func (c *readOnlyGadgetClient) List(_ context.Context, opts adapter.ListOptions) ([]fakeGadget, error) {
	return adapter.TruncateSlice(c.items, opts.Limit), nil
}

func (c *readOnlyGadgetClient) Get(_ context.Context, name string) (*fakeGadget, error) {
	for i := range c.items {
		if c.items[i].Name == name {
			return &c.items[i], nil
		}
	}
	return nil, adapter.ErrNotFound
}

func TestResource_UnsupportedVerbsResolveToErrUnsupported(t *testing.T) {
	client := &readOnlyGadgetClient{items: []fakeGadget{{Name: "g-1", Value: "one"}}}

	res := adapter.Resource[fakeGadget]{
		Group: "test.grafana.app", Version: "v1alpha1", Kind: "Gadget",
		NewClient: func(context.Context, adapter.ClientDeps) (any, error) { return client, nil },
	}

	p := adapter.NewProvider("fakeprovider", "Fake provider for tests", fakeDeps, res)
	regs := p.TypedRegistrations()
	require.Len(t, regs, 1)

	ra, err := regs[0].Factory(t.Context())
	require.NoError(t, err)

	// The implemented verbs work normally — no provider flags or nil-Fn
	// plumbing needed to make List/Get available.
	list, err := ra.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, list.Items, 1)

	got, err := ra.Get(t.Context(), "g-1", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "g-1", got.GetName())

	// The unimplemented verbs resolve to ErrUnsupported with no extra plumbing.
	_, err = ra.Create(t.Context(), buildGadgetUnstructured("g-2", "two"), metav1.CreateOptions{})
	require.ErrorIs(t, err, errors.ErrUnsupported)

	_, err = ra.Update(t.Context(), buildGadgetUnstructured("g-1", "updated"), metav1.UpdateOptions{})
	require.ErrorIs(t, err, errors.ErrUnsupported)

	err = ra.Delete(t.Context(), "g-1", metav1.DeleteOptions{})
	require.ErrorIs(t, err, errors.ErrUnsupported)
}

// --- AC-004: dry-run routes to Validator[T] when implemented, else unverified skip ---

// validatingGadgetClient implements Creator[fakeGadget] and
// Validator[fakeGadget], tracking which one was actually invoked.
type validatingGadgetClient struct {
	createCalled   bool
	validateCalled bool
}

func (c *validatingGadgetClient) Create(_ context.Context, item *fakeGadget) (*fakeGadget, error) {
	c.createCalled = true
	return item, nil
}

func (c *validatingGadgetClient) Validate(_ context.Context, _ []*fakeGadget) error {
	c.validateCalled = true
	return nil
}

// plainGadgetClient implements only Creator[fakeGadget] — no Validator.
type plainGadgetClient struct {
	createCalled bool
}

func (c *plainGadgetClient) Create(_ context.Context, item *fakeGadget) (*fakeGadget, error) {
	c.createCalled = true
	return item, nil
}

func TestResource_DryRunRouting(t *testing.T) {
	t.Run("Validator client routes dry-run to Validate, not Create", func(t *testing.T) {
		client := &validatingGadgetClient{}
		res := adapter.Resource[fakeGadget]{
			Group: "test.grafana.app", Version: "v1alpha1", Kind: "Gadget",
			NewClient: func(context.Context, adapter.ClientDeps) (any, error) { return client, nil },
		}
		p := adapter.NewProvider("fakeprovider", "Fake provider for tests", fakeDeps, res)
		ra, err := p.TypedRegistrations()[0].Factory(t.Context())
		require.NoError(t, err)

		_, err = ra.Create(t.Context(), buildGadgetUnstructured("g-1", "one"), metav1.CreateOptions{
			DryRun: []string{metav1.DryRunAll},
		})
		require.NoError(t, err)
		assert.True(t, client.validateCalled, "Validate must be called on dry-run")
		assert.False(t, client.createCalled, "Create must not be called on dry-run")
	})

	t.Run("non-Validator client skips the mutation on dry-run and reports it unverified", func(t *testing.T) {
		client := &plainGadgetClient{}
		res := adapter.Resource[fakeGadget]{
			Group: "test.grafana.app", Version: "v1alpha1", Kind: "Gadget",
			NewClient: func(context.Context, adapter.ClientDeps) (any, error) { return client, nil },
		}
		p := adapter.NewProvider("fakeprovider", "Fake provider for tests", fakeDeps, res)
		ra, err := p.TypedRegistrations()[0].Factory(t.Context())
		require.NoError(t, err)

		_, err = ra.Create(t.Context(), buildGadgetUnstructured("g-1", "one"), metav1.CreateOptions{
			DryRun: []string{metav1.DryRunAll},
		})
		require.ErrorIs(t, err, adapter.ErrDryRunUnverified, "dry-run without Validator must return ErrDryRunUnverified so the resource is reported as skipped")
		assert.False(t, client.createCalled, "Create must not be called on dry-run")
	})

	t.Run("non-dry-run still calls Create normally", func(t *testing.T) {
		client := &plainGadgetClient{}
		res := adapter.Resource[fakeGadget]{
			Group: "test.grafana.app", Version: "v1alpha1", Kind: "Gadget",
			NewClient: func(context.Context, adapter.ClientDeps) (any, error) { return client, nil },
		}
		p := adapter.NewProvider("fakeprovider", "Fake provider for tests", fakeDeps, res)
		ra, err := p.TypedRegistrations()[0].Factory(t.Context())
		require.NoError(t, err)

		_, err = ra.Create(t.Context(), buildGadgetUnstructured("g-1", "one"), metav1.CreateOptions{})
		require.NoError(t, err)
		assert.True(t, client.createCalled)
	})
}

// --- AC-011: Plural override propagates to discovery and the resources pipeline ---

func TestResource_PluralOverridePropagates(t *testing.T) {
	client := &readOnlyGadgetClient{}
	//nolint:unparam // Signature is fixed by Resource[T].NewClient; this fixture never fails.
	newClient := func(context.Context, adapter.ClientDeps) (any, error) { return client, nil }

	tests := []struct {
		name       string
		kind       string
		pluralOpt  string
		wantPlural string
	}{
		{
			name:       "naive deriver is correct without an override",
			kind:       "Widget",
			wantPlural: "widgets",
		},
		{
			name:       "naive deriver handles the y -> ies case without an override",
			kind:       "EscalationPolicy",
			wantPlural: "escalationpolicies",
		},
		{
			name:       "irregular plural requires the override",
			kind:       "Sheep",
			pluralOpt:  "sheep",
			wantPlural: "sheep",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := adapter.Resource[fakeGadget]{
				Group: "test.grafana.app", Version: "v1alpha1", Kind: tc.kind,
				Plural:    tc.pluralOpt,
				NewClient: newClient,
			}
			p := adapter.NewProvider("fakeprovider", "Fake provider for tests", fakeDeps, res)
			reg := p.TypedRegistrations()[0]

			// Propagates to the Registration used by discovery/RegisterAll.
			assert.Equal(t, tc.wantPlural, reg.Descriptor.Plural)

			// Propagates to the built adapter instance used by the resources
			// pipeline (get/push/pull/delete all read Descriptor() off this).
			ra, err := reg.Factory(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tc.wantPlural, ra.Descriptor().Plural)
		})
	}
}

// --- Schema/Example are derived, not hand-threaded ---

func TestResource_SchemaAndExampleAreDerived(t *testing.T) {
	client := &readOnlyGadgetClient{}
	res := adapter.Resource[fakeGadget]{
		Group: "test.grafana.app", Version: "v1alpha1", Kind: "Gadget",
		Example:   fakeGadget{Name: "my-gadget", Value: "demo"},
		NewClient: func(context.Context, adapter.ClientDeps) (any, error) { return client, nil },
	}

	p := adapter.NewProvider("fakeprovider", "Fake provider for tests", fakeDeps, res)
	reg := p.TypedRegistrations()[0]

	assert.NotNil(t, reg.Schema, "schema must be auto-derived from T, not hand-threaded")
	require.NotNil(t, reg.Example, "example must be derived from Resource.Example")
	assert.Contains(t, string(reg.Example), `"my-gadget"`)

	ra, err := reg.Factory(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, ra.Schema(), "AsAdapter's Schema() must never be nil (FR-016)")
	assert.Equal(t, reg.Example, ra.Example())
}
