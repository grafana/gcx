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

func TestFieldSelectCodec_PreservesArrayShape(t *testing.T) {
	codec := cmdio.NewFieldSelectCodec([]string{"name"})
	input := []map[string]any{
		{"name": "foo", "extra": "x"},
		{"name": "bar", "extra": "y"},
	}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, input))

	// Must be a JSON array, not {"items":[...]}
	var result []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result),
		"output must be a JSON array; got: %s", buf.String())
	require.Len(t, result, 2)
	assert.Equal(t, "foo", result[0]["name"])
	assert.Nil(t, result[0]["extra"], "non-selected field must be absent/null")
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

// TestFieldSelectCodec_WithValidator verifies that the validator is invoked before
// field extraction and that UnknownFieldSelectionError is returned for unknown fields.
func TestFieldSelectCodec_WithValidator(t *testing.T) {
	type item struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	// Build a validator from the item type.
	validator := cmdio.MakeFieldValidator(item{})
	require.NotNil(t, validator, "MakeFieldValidator must return a non-nil validator for a struct type")

	tests := []struct {
		name       string
		fields     []string
		value      any
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:   "valid fields — no error",
			fields: []string{"name", "status"},
			value:  item{Name: "foo", Status: "ok"},
		},
		{
			name:    "single unknown field — UnknownFieldSelectionError",
			fields:  []string{"bogus"},
			value:   item{Name: "foo"},
			wantErr: true,
		},
		{
			name:    "mix of valid and unknown — UnknownFieldSelectionError with offenders only",
			fields:  []string{"name", "bogus"},
			value:   item{Name: "foo"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := cmdio.NewFieldSelectCodecWithValidator(tc.fields, validator)
			var buf bytes.Buffer
			err := codec.Encode(&buf, tc.value)
			if tc.wantErr {
				require.Error(t, err)
				var fieldErr cmdio.UnknownFieldSelectionError
				require.ErrorAs(t, err, &fieldErr, "error must be UnknownFieldSelectionError")
				// All unknown fields must appear in the error.
				for _, f := range tc.fields {
					if f != "name" && f != "status" {
						assert.Contains(t, fieldErr.Fields, f)
					}
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMakeFieldValidator_StructType verifies that MakeFieldValidator returns a validator
// that accepts known fields and rejects unknown ones for a struct type.
func TestMakeFieldValidator_StructType(t *testing.T) {
	type myItem struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}

	validator := cmdio.MakeFieldValidator(myItem{})
	require.NotNil(t, validator)

	// Known fields pass.
	require.NoError(t, validator([]string{"name"}))
	require.NoError(t, validator([]string{"namespace"}))
	require.NoError(t, validator([]string{"name", "namespace"}))

	// Unknown field fails.
	err := validator([]string{"bogus"})
	require.Error(t, err)
	var fieldErr cmdio.UnknownFieldSelectionError
	require.ErrorAs(t, err, &fieldErr)
	assert.Equal(t, []string{"bogus"}, fieldErr.Fields)

	// Mix: only unknown fields appear in the error.
	err = validator([]string{"name", "unknown"})
	require.Error(t, err)
	require.ErrorAs(t, err, &fieldErr)
	assert.Equal(t, []string{"unknown"}, fieldErr.Fields)
}

// TestMakeFieldValidator_EnvelopeType verifies that MakeFieldValidator on a
// list-envelope struct builds the validator from item fields, not the envelope.
// (The sample passed must be the item type, not the envelope.)
func TestMakeFieldValidator_EnvelopeType(t *testing.T) {
	type innerItem struct {
		Name string `json:"name"`
	}
	type envelope struct {
		Items []innerItem `json:"items"`
	}

	// When passed the envelope, the validator sees "items" as the only field.
	validatorEnvelope := cmdio.MakeFieldValidator(envelope{})
	require.NotNil(t, validatorEnvelope)
	// "name" is NOT a top-level field of the envelope — rejected.
	err := validatorEnvelope([]string{"name"})
	require.Error(t, err, "envelope-level validator should reject item-level fields")

	// When passed the inner type, the validator sees "name" as a valid field.
	validatorItem := cmdio.MakeFieldValidator(innerItem{})
	require.NotNil(t, validatorItem)
	require.NoError(t, validatorItem([]string{"name"}))
}

// TestUnknownFieldSelectionError_Error verifies the error message format.
func TestUnknownFieldSelectionError_Error(t *testing.T) {
	err := cmdio.UnknownFieldSelectionError{Fields: []string{"bogus", "extra"}}
	msg := err.Error()
	assert.Contains(t, msg, "bogus")
	assert.Contains(t, msg, "extra")
	assert.Contains(t, msg, "--json list")
}
