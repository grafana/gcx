package output_test

import (
	"bytes"
	"encoding/json"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestFieldSelectCodec_SingleUnstructured(t *testing.T) {
	tests := []struct {
		name       string
		fields     []string
		obj        map[string]any
		wantFields map[string]any
	}{
		{
			name:   "extracts requested top-level fields",
			fields: []string{"name", "namespace"},
			obj: map[string]any{
				"name":      "foo",
				"namespace": "default",
				"kind":      "Dashboard",
			},
			wantFields: map[string]any{
				"name":      "foo",
				"namespace": "default",
			},
		},
		{
			name:   "missing field produces null",
			fields: []string{"nonexistent"},
			obj: map[string]any{
				"name": "foo",
			},
			wantFields: map[string]any{
				"nonexistent": nil,
			},
		},
		{
			name:   "dot-notation resolves nested field",
			fields: []string{"metadata.name"},
			obj: map[string]any{
				"metadata": map[string]any{
					"name":      "my-dashboard",
					"namespace": "default",
				},
			},
			wantFields: map[string]any{
				"metadata.name": "my-dashboard",
			},
		},
		{
			name:   "dot-notation on missing nested key produces null",
			fields: []string{"metadata.missing"},
			obj: map[string]any{
				"metadata": map[string]any{
					"name": "my-dashboard",
				},
			},
			wantFields: map[string]any{
				"metadata.missing": nil,
			},
		},
		{
			name:   "dot-notation on non-map intermediate produces null",
			fields: []string{"spec.title.nested"},
			obj: map[string]any{
				"spec": map[string]any{
					"title": "My Dashboard",
				},
			},
			wantFields: map[string]any{
				"spec.title.nested": nil,
			},
		},
		{
			name:   "multiple fields including missing",
			fields: []string{"name", "missing"},
			obj: map[string]any{
				"name": "foo",
			},
			wantFields: map[string]any{
				"name":    "foo",
				"missing": nil,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := cmdio.NewFieldSelectCodec(tc.fields)

			item := unstructured.Unstructured{Object: tc.obj}
			var buf bytes.Buffer
			err := codec.Encode(&buf, item)
			require.NoError(t, err)

			var got map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
			assert.Equal(t, tc.wantFields, got)
		})
	}
}

func TestFieldSelectCodec_ListWrapping(t *testing.T) {
	tests := []struct {
		name      string
		fields    []string
		items     []map[string]any
		wantItems []map[string]any
	}{
		{
			name:   "list of items wrapped in items key",
			fields: []string{"name"},
			items: []map[string]any{
				{"name": "foo", "kind": "Dashboard"},
				{"name": "bar", "kind": "Dashboard"},
			},
			wantItems: []map[string]any{
				{"name": "foo"},
				{"name": "bar"},
			},
		},
		{
			name:   "missing field in list items produces null",
			fields: []string{"nonexistent"},
			items: []map[string]any{
				{"name": "foo"},
			},
			wantItems: []map[string]any{
				{"nonexistent": nil},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := cmdio.NewFieldSelectCodec(tc.fields)

			list := unstructured.UnstructuredList{}
			for _, obj := range tc.items {
				list.Items = append(list.Items, unstructured.Unstructured{Object: obj})
			}

			var buf bytes.Buffer
			err := codec.Encode(&buf, list)
			require.NoError(t, err)

			var got map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

			rawItems, ok := got["items"]
			require.True(t, ok, "expected 'items' key in output")

			itemsSlice, ok := rawItems.([]any)
			require.True(t, ok)
			require.Len(t, itemsSlice, len(tc.wantItems))

			for i, wantItem := range tc.wantItems {
				gotItem, ok := itemsSlice[i].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, wantItem, gotItem)
			}
		})
	}
}

func TestFieldSelectCodec_PrintItemsType(t *testing.T) {
	type printItems struct {
		Items []map[string]any `json:"items"`
	}

	codec := cmdio.NewFieldSelectCodec([]string{"name"})

	input := printItems{
		Items: []map[string]any{
			{"name": "foo", "kind": "Dashboard"},
			{"name": "bar", "kind": "Folder"},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, input))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

	rawItems := got["items"]
	itemsSlice, ok := rawItems.([]any)
	require.True(t, ok)
	require.Len(t, itemsSlice, 2)

	for _, elem := range itemsSlice {
		m, ok := elem.(map[string]any)
		require.True(t, ok)
		assert.Contains(t, m, "name")
		assert.NotContains(t, m, "kind")
	}
}

func TestDiscoverFields(t *testing.T) {
	tests := []struct {
		name       string
		obj        map[string]any
		wantFields []string
	}{
		{
			name: "top-level fields returned",
			obj: map[string]any{
				"apiVersion": "v1",
				"kind":       "Dashboard",
				"metadata":   map[string]any{},
			},
			wantFields: []string{"apiVersion", "kind", "metadata"},
		},
		{
			name: "spec sub-fields expanded",
			obj: map[string]any{
				"spec": map[string]any{
					"title":       "My Dashboard",
					"description": "desc",
				},
			},
			wantFields: []string{"spec", "spec.description", "spec.title"},
		},
		{
			name: "fields returned in sorted order",
			obj: map[string]any{
				"z": "last",
				"a": "first",
				"m": "middle",
			},
			wantFields: []string{"a", "m", "z"},
		},
		{
			name:       "empty object",
			obj:        map[string]any{},
			wantFields: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cmdio.DiscoverFields(tc.obj)
			assert.Equal(t, tc.wantFields, got)
		})
	}
}
