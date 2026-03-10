package io_test

// json_fields_integration_test.go verifies end-to-end behavior of the --json
// field selection flag using FieldSelectCodec and Options directly with mock
// resource data. No real network calls are made.
//
// Acceptance criteria exercised:
//   - GIVEN a grafanactl command that returns resource data
//     WHEN --json metadata.name,spec is provided
//     THEN stdout contains a JSON object with only the requested fields
//   - GIVEN a grafanactl list command returning multiple resources
//     WHEN --json metadata.name is provided
//     THEN stdout contains {"items": [{"metadata.name": "..."}, ...]}
//   - GIVEN a grafanactl command that returns resource data
//     WHEN --json nonexistent is provided
//     THEN stdout contains a JSON object where nonexistent is null
//   - GIVEN a grafanactl command
//     WHEN both --json field1 and -o yaml are provided
//     THEN the command exits with a usage error

import (
	"bytes"
	"encoding/json"
	"testing"

	cmdio "github.com/grafana/grafanactl/cmd/grafanactl/io"
	"github.com/grafana/grafanactl/internal/terminal"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestJSONFieldSelection_SingleResource verifies that FieldSelectCodec
// extracts only the requested fields from a single resource.
func TestJSONFieldSelection_SingleResource(t *testing.T) {
	tests := []struct {
		name       string
		fields     []string
		obj        map[string]any
		wantKeys   []string
		wantAbsent []string
	}{
		{
			name:   "select name and kind fields",
			fields: []string{"name", "kind"},
			obj: map[string]any{
				"name":      "my-dashboard",
				"kind":      "Dashboard",
				"namespace": "default",
				"spec":      map[string]any{"title": "My Dashboard"},
			},
			wantKeys:   []string{"name", "kind"},
			wantAbsent: []string{"namespace", "spec"},
		},
		{
			name:   "dot-notation selects nested field",
			fields: []string{"metadata.name", "kind"},
			obj: map[string]any{
				"kind": "Dashboard",
				"metadata": map[string]any{
					"name":      "my-dashboard",
					"namespace": "default",
				},
			},
			wantKeys:   []string{"metadata.name", "kind"},
			wantAbsent: []string{"metadata"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := cmdio.NewFieldSelectCodec(tc.fields)

			item := unstructured.Unstructured{Object: tc.obj}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, item))

			var got map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

			for _, key := range tc.wantKeys {
				assert.Contains(t, got, key, "expected field %q in output", key)
			}
			for _, key := range tc.wantAbsent {
				assert.NotContains(t, got, key, "unexpected field %q in output", key)
			}
		})
	}
}

// TestJSONFieldSelection_MultipleResources verifies that a list of resources
// is wrapped in {"items": [...]} with only the selected fields per item.
//
// Acceptance criterion:
//
//	GIVEN a grafanactl list command returning multiple resources
//	WHEN --json metadata.name is provided
//	THEN stdout contains {"items": [{"metadata.name": "..."}, ...]}
func TestJSONFieldSelection_MultipleResources(t *testing.T) {
	codec := cmdio.NewFieldSelectCodec([]string{"name", "kind"})

	list := unstructured.UnstructuredList{}
	list.Items = []unstructured.Unstructured{
		{Object: map[string]any{"name": "foo", "kind": "Dashboard", "namespace": "default"}},
		{Object: map[string]any{"name": "bar", "kind": "Folder", "namespace": "default"}},
	}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, list))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

	rawItems, ok := got["items"]
	require.True(t, ok, "expected 'items' key in list output")

	items, ok := rawItems.([]any)
	require.True(t, ok, "expected 'items' to be an array")
	require.Len(t, items, 2)

	wantNames := []string{"foo", "bar"}
	for i, item := range items {
		m, ok := item.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, wantNames[i], m["name"], "item %d name mismatch", i)
		assert.Equal(t, "Dashboard", m["kind"]) // first item only; skip second in loop
		assert.NotContains(t, m, "namespace", "namespace should not be in output")
		break // just verify the first item's structure; second is verified by Len check
	}

	// Verify second item
	second, ok := items[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "bar", second["name"])
	assert.Equal(t, "Folder", second["kind"])
	assert.NotContains(t, second, "namespace")
}

// TestJSONFieldSelection_MissingFieldIsNull verifies that a requested field
// that does not exist in the resource is output as null (not omitted).
//
// Acceptance criterion:
//
//	GIVEN a grafanactl command that returns resource data
//	WHEN --json nonexistent is provided
//	THEN stdout contains a JSON object where nonexistent is null
func TestJSONFieldSelection_MissingFieldIsNull(t *testing.T) {
	codec := cmdio.NewFieldSelectCodec([]string{"name", "nonexistent"})

	item := unstructured.Unstructured{Object: map[string]any{
		"name": "my-dashboard",
	}}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, item))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

	assert.Equal(t, "my-dashboard", got["name"])

	// The field must be present in the output (not omitted), with a null value.
	val, exists := got["nonexistent"]
	assert.True(t, exists, "missing field must be present in output with null value")
	assert.Nil(t, val, "missing field value must be null")
}

// TestJSONFieldSelection_MutualExclusionWithOutput verifies that providing
// both --json and -o returns a usage error (FR-009).
//
// Acceptance criterion:
//
//	GIVEN a grafanactl command
//	WHEN both --json field1 and -o yaml are provided
//	THEN the command exits with a usage error
func TestJSONFieldSelection_MutualExclusionWithOutput(t *testing.T) {
	opts := &cmdio.Options{}
	opts.RegisterCustomCodec("yaml", &dummyCodec{})

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)

	require.NoError(t, flags.Set("json", "name"))
	require.NoError(t, flags.Set("output", "yaml"))

	err := opts.Validate()
	require.Error(t, err, "expected error when --json and -o are both provided")
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestJSONFieldSelection_OptionsBindingReflectsFields verifies that
// Options.JSONFields is populated correctly after BindFlags + Validate.
func TestJSONFieldSelection_OptionsBindingReflectsFields(t *testing.T) {
	tests := []struct {
		name           string
		jsonFlag       string
		wantFields     []string
		wantDiscovery  bool
		wantOutputJSON bool
	}{
		{
			name:           "single field",
			jsonFlag:       "name",
			wantFields:     []string{"name"},
			wantOutputJSON: true,
		},
		{
			name:           "multiple comma-separated fields",
			jsonFlag:       "name,kind,metadata.namespace",
			wantFields:     []string{"name", "kind", "metadata.namespace"},
			wantOutputJSON: true,
		},
		{
			name:          "discovery sentinel ?",
			jsonFlag:      "?",
			wantDiscovery: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			terminal.ResetForTesting()
			t.Cleanup(terminal.ResetForTesting)

			opts := &cmdio.Options{}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.BindFlags(flags)

			require.NoError(t, flags.Set("json", tc.jsonFlag))
			require.NoError(t, opts.Validate())

			assert.Equal(t, tc.wantFields, opts.JSONFields)
			assert.Equal(t, tc.wantDiscovery, opts.JSONDiscovery)
			if tc.wantOutputJSON {
				assert.Equal(t, "json", opts.OutputFormat)
			}
		})
	}
}

// TestJSONFieldSelection_DiscoverFields verifies DiscoverFields returns sorted
// field paths including spec sub-field expansions.
func TestJSONFieldSelection_DiscoverFields(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Dashboard",
		"metadata": map[string]any{
			"name":      "test",
			"namespace": "default",
		},
		"spec": map[string]any{
			"title":       "My Dashboard",
			"description": "A test dashboard",
		},
	}

	fields := cmdio.DiscoverFields(obj)

	// All top-level fields must be present.
	assert.Contains(t, fields, "apiVersion")
	assert.Contains(t, fields, "kind")
	assert.Contains(t, fields, "metadata")
	assert.Contains(t, fields, "spec")

	// spec sub-fields must be expanded.
	assert.Contains(t, fields, "spec.title")
	assert.Contains(t, fields, "spec.description")

	// Fields must be sorted.
	for i := 1; i < len(fields); i++ {
		assert.LessOrEqual(t, fields[i-1], fields[i], "fields must be sorted alphabetically")
	}
}
