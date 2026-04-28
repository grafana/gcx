package adapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestWidget is a simple domain object used across all tests.
//
//nolint:recvcheck // Mixed receivers are intentional for testing TypedCRUD compatibility.
type TestWidget struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Color  string `json:"color"`
	Secret string `json:"secret"`
}

// ResourceIdentity implementation for TestWidget.
func (w TestWidget) GetResourceName() string   { return w.ID }
func (w *TestWidget) SetResourceName(n string) { w.ID = n }

var widgetDesc = resources.Descriptor{ //nolint:gochecknoglobals // Test fixture.
	GroupVersion: schema.GroupVersion{Group: "test.grafana.app", Version: "v1"},
	Kind:         "Widget",
	Singular:     "widget",
	Plural:       "widgets",
}

// newWidgetCRUD returns a TypedCRUD configured for TestWidget with sensible defaults.
func newWidgetCRUD(widgets []TestWidget) *adapter.TypedCRUD[TestWidget] {
	return &adapter.TypedCRUD[TestWidget]{
		Namespace:   "stack-1",
		StripFields: []string{"id", "secret"},
		Descriptor:  widgetDesc,
		Aliases:     []string{"wdg"},
		ListFn: func(_ context.Context, _ int64) ([]TestWidget, error) {
			return widgets, nil
		},
		GetFn: func(_ context.Context, name string) (*TestWidget, error) {
			for i := range widgets {
				if widgets[i].ID == name {
					return &widgets[i], nil
				}
			}
			return nil, fmt.Errorf("widget %q: %w", name, adapter.ErrNotFound)
		},
	}
}

// buildWidgetUnstructured builds a minimal unstructured object for Create/Update tests.
func buildWidgetUnstructured(name, widgetName, color string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "test.grafana.app/v1",
			"kind":       "Widget",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "stack-1",
			},
			"spec": map[string]any{
				"name":  widgetName,
				"color": color,
			},
		},
	}
}

func TestTypedCRUD_List(t *testing.T) {
	widgets := []TestWidget{
		{ID: "w-1", Name: "Alpha", Color: "red", Secret: "s1"},
		{ID: "w-2", Name: "Beta", Color: "blue", Secret: "s2"},
	}
	crud := newWidgetCRUD(widgets)
	a := crud.AsAdapter()

	result, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	for i, item := range result.Items {
		w := widgets[i]

		assert.Equal(t, "test.grafana.app/v1", item.GetAPIVersion())
		assert.Equal(t, "Widget", item.GetKind())
		assert.Equal(t, w.ID, item.GetName())
		assert.Equal(t, "stack-1", item.GetNamespace())

		spec, ok := item.Object["spec"].(map[string]any)
		require.True(t, ok, "spec should be a map")

		// StripFields should be removed.
		assert.NotContains(t, spec, "id")
		assert.NotContains(t, spec, "secret")

		// Remaining spec fields should be present.
		assert.Equal(t, w.Name, spec["name"])
		assert.Equal(t, w.Color, spec["color"])
	}
}

func TestTypedCRUD_Get(t *testing.T) {
	widgets := []TestWidget{
		{ID: "w-1", Name: "Alpha", Color: "red", Secret: "s1"},
	}
	crud := newWidgetCRUD(widgets)
	a := crud.AsAdapter()

	t.Run("returns existing resource", func(t *testing.T) {
		item, err := a.Get(t.Context(), "w-1", metav1.GetOptions{})
		require.NoError(t, err)

		assert.Equal(t, "test.grafana.app/v1", item.GetAPIVersion())
		assert.Equal(t, "Widget", item.GetKind())
		assert.Equal(t, "w-1", item.GetName())
		assert.Equal(t, "stack-1", item.GetNamespace())

		spec, ok := item.Object["spec"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Alpha", spec["name"])
		assert.NotContains(t, spec, "id")
	})

	t.Run("returns K8s NotFound for missing resource", func(t *testing.T) {
		_, err := a.Get(t.Context(), "w-missing", metav1.GetOptions{})
		require.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err), "adapter Get must return Kubernetes-style NotFound, got: %v", err)
	})
}

func TestTypedCRUD_Create(t *testing.T) {
	var createdItem *TestWidget

	crud := newWidgetCRUD(nil)
	crud.CreateFn = func(_ context.Context, item *TestWidget) (*TestWidget, error) {
		createdItem = item
		result := *item
		result.ID = "w-new"
		return &result, nil
	}

	a := crud.AsAdapter()
	input := buildWidgetUnstructured("w-input", "Gamma", "green")

	result, err := a.Create(t.Context(), input, metav1.CreateOptions{})
	require.NoError(t, err)

	// SetResourceName should have set ID from metadata.name.
	require.NotNil(t, createdItem)
	assert.Equal(t, "w-input", createdItem.ID)
	assert.Equal(t, "Gamma", createdItem.Name)
	assert.Equal(t, "green", createdItem.Color)

	// Result should be the re-wrapped created object.
	assert.Equal(t, "w-new", result.GetName())

	spec, ok := result.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "green", spec["color"])
}

func TestTypedCRUD_Update(t *testing.T) {
	var updatedName string
	var updatedItem *TestWidget

	crud := newWidgetCRUD(nil)
	crud.UpdateFn = func(_ context.Context, name string, item *TestWidget) (*TestWidget, error) {
		updatedName = name
		updatedItem = item
		result := *item
		return &result, nil
	}

	a := crud.AsAdapter()
	input := buildWidgetUnstructured("w-existing", "Delta", "yellow")

	result, err := a.Update(t.Context(), input, metav1.UpdateOptions{})
	require.NoError(t, err)

	assert.Equal(t, "w-existing", updatedName)
	require.NotNil(t, updatedItem)
	assert.Equal(t, "w-existing", updatedItem.ID) // SetResourceName applied
	assert.Equal(t, "Delta", updatedItem.Name)

	assert.Equal(t, "w-existing", result.GetName())
}

func TestTypedCRUD_Delete(t *testing.T) {
	var deletedName string

	crud := newWidgetCRUD(nil)
	crud.DeleteFn = func(_ context.Context, name string) error {
		deletedName = name
		return nil
	}

	a := crud.AsAdapter()
	err := a.Delete(t.Context(), "w-del", metav1.DeleteOptions{})
	require.NoError(t, err)
	assert.Equal(t, "w-del", deletedName)
}

func TestTypedCRUD_NilFunctions(t *testing.T) {
	tests := []struct {
		name string
		fn   func(adapter.ResourceAdapter) error
	}{
		{
			name: "nil CreateFn returns ErrUnsupported",
			fn: func(a adapter.ResourceAdapter) error {
				_, err := a.Create(t.Context(), buildWidgetUnstructured("x", "x", "x"), metav1.CreateOptions{})
				return err
			},
		},
		{
			name: "nil UpdateFn returns ErrUnsupported",
			fn: func(a adapter.ResourceAdapter) error {
				_, err := a.Update(t.Context(), buildWidgetUnstructured("x", "x", "x"), metav1.UpdateOptions{})
				return err
			},
		},
		{
			name: "nil DeleteFn returns ErrUnsupported",
			fn: func(a adapter.ResourceAdapter) error {
				return a.Delete(t.Context(), "x", metav1.DeleteOptions{})
			},
		},
	}

	// crud has no CreateFn, UpdateFn, or DeleteFn set.
	crud := newWidgetCRUD(nil)
	a := crud.AsAdapter()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn(a)
			assert.ErrorIs(t, err, errors.ErrUnsupported)
		})
	}
}

func TestTypedCRUD_NilListFn(t *testing.T) {
	// When ListFn is nil, both typed List and adapter List should return ErrUnsupported.
	crud := &adapter.TypedCRUD[TestWidget]{
		Namespace:  "stack-1",
		Descriptor: widgetDesc,
		GetFn: func(_ context.Context, name string) (*TestWidget, error) {
			return &TestWidget{ID: name, Name: "Alpha"}, nil
		},
		// ListFn intentionally nil
	}

	t.Run("typed List returns ErrUnsupported", func(t *testing.T) {
		_, err := crud.List(t.Context(), 0)
		assert.ErrorIs(t, err, errors.ErrUnsupported)
	})

	t.Run("adapter List returns ErrUnsupported", func(t *testing.T) {
		a := crud.AsAdapter()
		_, err := a.List(t.Context(), metav1.ListOptions{})
		assert.ErrorIs(t, err, errors.ErrUnsupported)
	})

	t.Run("Get still works when ListFn is nil", func(t *testing.T) {
		obj, err := crud.Get(t.Context(), "w-1")
		require.NoError(t, err)
		assert.Equal(t, "w-1", obj.GetName())
	})
}

func TestTypedCRUD_NilGetFn_FallbackToList(t *testing.T) {
	// When GetFn is nil but ListFn is set, Get should fall back to
	// listing all items and filtering by name (client-side emulation).
	widgets := []TestWidget{
		{ID: "w-1", Name: "Alpha", Color: "red"},
		{ID: "w-2", Name: "Beta", Color: "blue"},
		{ID: "w-3", Name: "Gamma", Color: "green"},
	}

	crud := &adapter.TypedCRUD[TestWidget]{
		Namespace:  "stack-1",
		Descriptor: widgetDesc,
		Aliases:    []string{"wdg"},
		ListFn: func(_ context.Context, _ int64) ([]TestWidget, error) {
			return widgets, nil
		},
		// GetFn intentionally nil — should fall back to list + filter
	}

	t.Run("typed Get finds existing item by name", func(t *testing.T) {
		obj, err := crud.Get(t.Context(), "w-2")
		require.NoError(t, err)
		assert.Equal(t, "w-2", obj.GetName())
		assert.Equal(t, "Beta", obj.Spec.Name)
		assert.Equal(t, "blue", obj.Spec.Color)
	})

	t.Run("typed Get returns ErrNotFound for missing item", func(t *testing.T) {
		_, err := crud.Get(t.Context(), "w-nonexistent")
		require.Error(t, err)
		assert.ErrorIs(t, err, adapter.ErrNotFound)
	})

	t.Run("adapter Get finds existing item by name", func(t *testing.T) {
		a := crud.AsAdapter()
		item, err := a.Get(t.Context(), "w-2", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "w-2", item.GetName())

		spec, ok := item.Object["spec"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Beta", spec["name"])
	})

	t.Run("adapter Get returns K8s NotFound for missing item", func(t *testing.T) {
		a := crud.AsAdapter()
		_, err := a.Get(t.Context(), "w-nonexistent", metav1.GetOptions{})
		require.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err), "expected Kubernetes-style NotFound error, got: %v", err)
	})

	t.Run("List still works normally", func(t *testing.T) {
		result, err := crud.List(t.Context(), 0)
		require.NoError(t, err)
		assert.Len(t, result, 3)
	})
}

func TestTypedCRUD_NilGetFn_NilListFn(t *testing.T) {
	// When both ListFn and GetFn are nil, Get should return ErrUnsupported
	// (cannot fall back to list either).
	crud := &adapter.TypedCRUD[TestWidget]{
		Namespace:  "stack-1",
		Descriptor: widgetDesc,
	}

	t.Run("typed Get returns ErrUnsupported", func(t *testing.T) {
		_, err := crud.Get(t.Context(), "w-1")
		assert.ErrorIs(t, err, errors.ErrUnsupported)
	})

	t.Run("typed List returns ErrUnsupported", func(t *testing.T) {
		_, err := crud.List(t.Context(), 0)
		assert.ErrorIs(t, err, errors.ErrUnsupported)
	})
}

func TestTypedCRUD_MetadataFn(t *testing.T) {
	tests := []struct {
		name       string
		metadataFn func(TestWidget) map[string]any
		wantUID    string
		wantName   string // metadata.name should always be widget ID
	}{
		{
			name: "extra metadata merged",
			metadataFn: func(w TestWidget) map[string]any {
				return map[string]any{
					"uid":       "extra-uid",
					"name":      "should-be-ignored",
					"namespace": "should-be-ignored",
				}
			},
			wantUID:  "extra-uid",
			wantName: "w-1",
		},
		{
			name:       "nil MetadataFn only has name and namespace",
			metadataFn: nil,
			wantName:   "w-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			widgets := []TestWidget{
				{ID: "w-1", Name: "Alpha", Color: "red"},
			}
			crud := newWidgetCRUD(widgets)
			crud.MetadataFn = tt.metadataFn
			a := crud.AsAdapter()

			result, err := a.List(t.Context(), metav1.ListOptions{})
			require.NoError(t, err)
			require.Len(t, result.Items, 1)

			item := result.Items[0]
			assert.Equal(t, tt.wantName, item.GetName(), "name must not be overwritten")
			assert.Equal(t, "stack-1", item.GetNamespace(), "namespace must not be overwritten")

			if tt.wantUID != "" {
				md, ok := item.Object["metadata"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, tt.wantUID, md["uid"])
			}
		})
	}
}

func TestTypedCRUD_DescriptorAndAliases(t *testing.T) {
	crud := newWidgetCRUD(nil)
	a := crud.AsAdapter()

	assert.Equal(t, widgetDesc, a.Descriptor())
	assert.Equal(t, []string{"wdg"}, a.Aliases())
}

func TestTypedRegistration_ToRegistration(t *testing.T) {
	desc := widgetDesc
	gvk := desc.GroupVersionKind()

	reg := adapter.TypedRegistration[TestWidget]{
		Descriptor: desc,
		Aliases:    []string{"wdg"},
		GVK:        gvk,
		Factory: func(_ context.Context) (*adapter.TypedCRUD[TestWidget], error) {
			widgets := []TestWidget{
				{ID: "w-1", Name: "Alpha", Color: "red"},
			}
			return newWidgetCRUD(widgets), nil
		},
	}

	registration := reg.ToRegistration()

	// Verify metadata fields pass through.
	assert.Equal(t, desc, registration.Descriptor)
	assert.Equal(t, []string{"wdg"}, registration.Aliases)
	assert.Equal(t, gvk, registration.GVK)

	// Verify the factory produces a working adapter.
	a, err := registration.Factory(t.Context())
	require.NoError(t, err)

	result, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "w-1", result.Items[0].GetName())
}

func TestTypedRegistration_SchemaExampleRoundTrip(t *testing.T) {
	testSchema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	testExample := json.RawMessage(`{"apiVersion":"test.grafana.app/v1","kind":"Widget","spec":{"name":"example"}}`)

	tests := []struct {
		name        string
		schema      json.RawMessage
		example     json.RawMessage
		wantSchema  json.RawMessage
		wantExample json.RawMessage
	}{
		{
			name:        "both schema and example set",
			schema:      testSchema,
			example:     testExample,
			wantSchema:  testSchema,
			wantExample: testExample,
		},
		{
			name:        "schema only",
			schema:      testSchema,
			example:     nil,
			wantSchema:  testSchema,
			wantExample: nil,
		},
		{
			name:        "example only",
			schema:      nil,
			example:     testExample,
			wantSchema:  nil,
			wantExample: testExample,
		},
		{
			name:        "neither set",
			schema:      nil,
			example:     nil,
			wantSchema:  nil,
			wantExample: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := adapter.TypedRegistration[TestWidget]{
				Descriptor: widgetDesc,
				Aliases:    []string{"wdg"},
				GVK:        widgetDesc.GroupVersionKind(),
				Schema:     tt.schema,
				Example:    tt.example,
				Factory: func(_ context.Context) (*adapter.TypedCRUD[TestWidget], error) {
					return newWidgetCRUD(nil), nil
				},
			}

			registration := reg.ToRegistration()
			a, err := registration.Factory(t.Context())
			require.NoError(t, err)

			assert.Equal(t, tt.wantSchema, a.Schema())
			assert.Equal(t, tt.wantExample, a.Example())
		})
	}
}

// --- Tests for TypedObject and typed methods ---

func TestTypedCRUD_TypedList(t *testing.T) {
	widgets := []TestWidget{
		{ID: "w-1", Name: "Alpha", Color: "red"},
		{ID: "w-2", Name: "Beta", Color: "blue"},
	}
	crud := newWidgetCRUD(widgets)

	result, err := crud.List(t.Context(), 0)
	require.NoError(t, err)
	require.Len(t, result, 2)

	for i, obj := range result {
		w := widgets[i]
		assert.Equal(t, "test.grafana.app/v1", obj.APIVersion)
		assert.Equal(t, "Widget", obj.Kind)
		assert.Equal(t, w.ID, obj.GetName())
		assert.Equal(t, "stack-1", obj.GetNamespace())
		assert.Equal(t, w.Name, obj.Spec.Name)
		assert.Equal(t, w.Color, obj.Spec.Color)
	}
}

func TestTypedCRUD_TypedGet(t *testing.T) {
	widgets := []TestWidget{
		{ID: "w-1", Name: "Alpha", Color: "red"},
	}
	crud := newWidgetCRUD(widgets)

	obj, err := crud.Get(t.Context(), "w-1")
	require.NoError(t, err)
	assert.Equal(t, "w-1", obj.GetName())
	assert.Equal(t, "Alpha", obj.Spec.Name)
}

func TestTypedCRUD_TypedCreate(t *testing.T) {
	crud := newWidgetCRUD(nil)
	crud.CreateFn = func(_ context.Context, item *TestWidget) (*TestWidget, error) {
		result := *item
		result.ID = "w-new"
		return &result, nil
	}

	input := adapter.TypedObject[TestWidget]{
		Spec: TestWidget{Name: "Gamma", Color: "green"},
	}

	result, err := crud.Create(t.Context(), &input)
	require.NoError(t, err)
	assert.Equal(t, "w-new", result.GetName())
	assert.Equal(t, "green", result.Spec.Color)
}

func TestTypedCRUD_TypedCreateNil(t *testing.T) {
	crud := newWidgetCRUD(nil)
	// CreateFn is nil

	input := adapter.TypedObject[TestWidget]{
		Spec: TestWidget{Name: "X"},
	}

	_, err := crud.Create(t.Context(), &input)
	assert.ErrorIs(t, err, errors.ErrUnsupported)
}

func TestTypedCRUD_TypedDelete(t *testing.T) {
	var deleted string
	crud := newWidgetCRUD(nil)
	crud.DeleteFn = func(_ context.Context, name string) error {
		deleted = name
		return nil
	}

	err := crud.Delete(t.Context(), "w-1")
	require.NoError(t, err)
	assert.Equal(t, "w-1", deleted)
}

func TestTypedCRUD_DeleteDryRun(t *testing.T) {
	deleteCalled := false
	crud := newWidgetCRUD(nil)
	crud.DeleteFn = func(_ context.Context, _ string) error {
		deleteCalled = true
		return nil
	}

	a := crud.AsAdapter()

	t.Run("skips DeleteFn when DryRun is flag is set", func(t *testing.T) {
		deleteCalled = false
		err := a.Delete(t.Context(), "w-1", metav1.DeleteOptions{
			DryRun: []string{metav1.DryRunAll},
		})
		require.NoError(t, err)
		assert.False(t, deleteCalled, "DeleteFn should not be called during dry run")
	})

	t.Run("calls DeleteFn without DryRun flag set", func(t *testing.T) {
		deleteCalled = false
		err := a.Delete(t.Context(), "w-1", metav1.DeleteOptions{})
		require.NoError(t, err)
		assert.True(t, deleteCalled, "DeleteFn should be called without dry run")
	})
}

func TestTypedCRUD_ResourceIdentity(t *testing.T) {
	// TypedCRUD should use GetResourceName() from the ResourceIdentity interface.
	widgets := []TestWidget{
		{ID: "w-1", Name: "Alpha", Color: "red"},
	}
	crud := &adapter.TypedCRUD[TestWidget]{
		Namespace: "stack-1",
		ListFn:    func(_ context.Context, _ int64) ([]TestWidget, error) { return widgets, nil },
		GetFn: func(_ context.Context, name string) (*TestWidget, error) {
			return &widgets[0], nil
		},
		Descriptor: widgetDesc,
	}

	// Test typed List uses GetResourceName()
	result, err := crud.List(t.Context(), 0)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "w-1", result[0].GetName(), "should use GetResourceName() from ResourceIdentity")

	// Test AsAdapter also works
	a := crud.AsAdapter()
	list, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, "w-1", list.Items[0].GetName())
}

func TestTypedCRUD_SetResourceName(t *testing.T) {
	// TypedCRUD should use SetResourceName() from the ResourceIdentity interface.
	crud := &adapter.TypedCRUD[TestWidget]{
		Namespace:   "stack-1",
		StripFields: []string{"id"},
		Descriptor:  widgetDesc,
		CreateFn: func(_ context.Context, item *TestWidget) (*TestWidget, error) {
			return item, nil
		},
	}

	a := crud.AsAdapter()
	input := buildWidgetUnstructured("restored-id", "TestName", "red")

	result, err := a.Create(t.Context(), input, metav1.CreateOptions{})
	require.NoError(t, err)
	assert.Equal(t, "restored-id", result.GetName(), "SetResourceName should set ID via ResourceIdentity")
}

func TestTypedCRUD_CreateDryRun(t *testing.T) {
	createCalled := false
	crud := newWidgetCRUD(nil)
	crud.CreateFn = func(_ context.Context, _ *TestWidget) (*TestWidget, error) {
		createCalled = true
		return &TestWidget{ID: "w-new"}, nil
	}

	a := crud.AsAdapter()
	input := buildWidgetUnstructured("w-input", "Gamma", "green")

	t.Run("skips CreateFn when DryRun is set", func(t *testing.T) {
		createCalled = false
		result, err := a.Create(t.Context(), input, metav1.CreateOptions{
			DryRun: []string{metav1.DryRunAll},
		})
		require.NoError(t, err)
		assert.False(t, createCalled, "CreateFn must not be called during dry run")
		assert.Equal(t, "w-input", result.GetName())
	})

	t.Run("calls CreateFn without DryRun", func(t *testing.T) {
		createCalled = false
		_, err := a.Create(t.Context(), input, metav1.CreateOptions{})
		require.NoError(t, err)
		assert.True(t, createCalled, "CreateFn must be called without dry run")
	})
}

func TestTypedCRUD_UpdateDryRun(t *testing.T) {
	updateCalled := false
	crud := newWidgetCRUD(nil)
	crud.UpdateFn = func(_ context.Context, _ string, _ *TestWidget) (*TestWidget, error) {
		updateCalled = true
		return &TestWidget{ID: "w-existing"}, nil
	}

	a := crud.AsAdapter()
	input := buildWidgetUnstructured("w-existing", "Delta", "yellow")

	t.Run("skips UpdateFn when DryRun is set", func(t *testing.T) {
		updateCalled = false
		result, err := a.Update(t.Context(), input, metav1.UpdateOptions{
			DryRun: []string{metav1.DryRunAll},
		})
		require.NoError(t, err)
		assert.False(t, updateCalled, "UpdateFn must not be called during dry run")
		assert.Equal(t, "w-existing", result.GetName())
	})

	t.Run("calls UpdateFn without DryRun", func(t *testing.T) {
		updateCalled = false
		_, err := a.Update(t.Context(), input, metav1.UpdateOptions{})
		require.NoError(t, err)
		assert.True(t, updateCalled, "UpdateFn must be called without dry run")
	})
}

func TestTypedCRUD_DryRunWithValidateFn(t *testing.T) {
	t.Run("calls ValidateFn on Create dry run", func(t *testing.T) {
		validateCalled := false
		crud := newWidgetCRUD(nil)
		crud.CreateFn = func(_ context.Context, _ *TestWidget) (*TestWidget, error) {
			t.Fatal("CreateFn must not be called during dry run")
			return nil, errors.New("unreachable")
		}
		crud.ValidateFn = func(_ context.Context, items []*TestWidget) error {
			validateCalled = true
			require.Len(t, items, 1)
			assert.Equal(t, "Gamma", items[0].Name)
			return nil
		}

		a := crud.AsAdapter()
		input := buildWidgetUnstructured("w-input", "Gamma", "green")
		_, err := a.Create(t.Context(), input, metav1.CreateOptions{
			DryRun: []string{metav1.DryRunAll},
		})
		require.NoError(t, err)
		assert.True(t, validateCalled, "ValidateFn must be called during dry run")
	})

	t.Run("calls ValidateFn on Update dry run", func(t *testing.T) {
		validateCalled := false
		crud := newWidgetCRUD(nil)
		crud.UpdateFn = func(_ context.Context, _ string, _ *TestWidget) (*TestWidget, error) {
			t.Fatal("UpdateFn must not be called during dry run")
			return nil, errors.New("unreachable")
		}
		crud.ValidateFn = func(_ context.Context, items []*TestWidget) error {
			validateCalled = true
			require.Len(t, items, 1)
			assert.Equal(t, "Delta", items[0].Name)
			return nil
		}

		a := crud.AsAdapter()
		input := buildWidgetUnstructured("w-existing", "Delta", "yellow")
		_, err := a.Update(t.Context(), input, metav1.UpdateOptions{
			DryRun: []string{metav1.DryRunAll},
		})
		require.NoError(t, err)
		assert.True(t, validateCalled, "ValidateFn must be called during dry run")
	})

	t.Run("propagates ValidateFn error", func(t *testing.T) {
		crud := newWidgetCRUD(nil)
		crud.CreateFn = func(_ context.Context, item *TestWidget) (*TestWidget, error) {
			return item, nil
		}
		crud.ValidateFn = func(_ context.Context, _ []*TestWidget) error {
			return errors.New("invalid aggregation type")
		}

		a := crud.AsAdapter()
		input := buildWidgetUnstructured("w-input", "Gamma", "green")
		_, err := a.Create(t.Context(), input, metav1.CreateOptions{
			DryRun: []string{metav1.DryRunAll},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid aggregation type")
	})
}

func TestTypedObject_JSONSerialization(t *testing.T) {
	obj := adapter.TypedObject[TestWidget]{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "test.grafana.app/v1",
			Kind:       "Widget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "w-1",
			Namespace: "stack-1",
		},
		Spec: TestWidget{ID: "w-1", Name: "Alpha", Color: "red"},
	}

	data, err := json.Marshal(obj)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Equal(t, "test.grafana.app/v1", m["apiVersion"])
	assert.Equal(t, "Widget", m["kind"])
	assert.Contains(t, m, "metadata")
	assert.Contains(t, m, "spec")

	spec, ok := m["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Alpha", spec["name"])
}
