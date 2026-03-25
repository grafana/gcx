package adapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestWidget is a simple domain object used across all tests.
type TestWidget struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Color  string `json:"color"`
	Secret string `json:"secret"`
}

var widgetDesc = resources.Descriptor{ //nolint:gochecknoglobals // Test fixture.
	GroupVersion: schema.GroupVersion{Group: "test.grafana.app", Version: "v1"},
	Kind:         "Widget",
	Singular:     "widget",
	Plural:       "widgets",
}

// newWidgetCRUD returns a TypedCRUD configured for TestWidget with sensible defaults.
func newWidgetCRUD(widgets []TestWidget) *adapter.TypedCRUD[TestWidget] {
	return &adapter.TypedCRUD[TestWidget]{
		NameFn:      func(w TestWidget) string { return w.ID },
		Namespace:   "stack-1",
		StripFields: []string{"id", "secret"},
		RestoreNameFn: func(name string, w *TestWidget) {
			w.ID = name
		},
		Descriptor: widgetDesc,
		Aliases:    []string{"wdg"},
		ListFn: func(_ context.Context) ([]TestWidget, error) {
			return widgets, nil
		},
		GetFn: func(_ context.Context, name string) (*TestWidget, error) {
			for i := range widgets {
				if widgets[i].ID == name {
					return &widgets[i], nil
				}
			}
			return nil, errors.New("not found")
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

	// RestoreNameFn should have set ID from metadata.name.
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
	assert.Equal(t, "w-existing", updatedItem.ID) // RestoreNameFn applied
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
