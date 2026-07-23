package output_test

// encode_json_fields_test.go tests the centralized JSON field selection and
// discovery in Options.Encode(). These verify that provider commands (and any
// caller that goes through Encode) get --json support automatically.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// helper to create Options with JSONFields set and flags bound.
func optsWithJSONFields(t *testing.T, fields []string) *cmdio.Options {
	t.Helper()
	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Set("json", strings.Join(fields, ",")))
	require.NoError(t, opts.Validate())
	return opts
}

// helper to create Options with JSONDiscovery set.
func optsWithJSONDiscovery(t *testing.T) *cmdio.Options {
	t.Helper()
	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Set("json", "?"))
	require.NoError(t, opts.Validate())
	return opts
}

func TestEncode_JSONFields_SingleObject(t *testing.T) {
	opts := optsWithJSONFields(t, []string{"name"})

	item := unstructured.Unstructured{Object: map[string]any{
		"name": "my-slo",
		"uuid": "abc-123",
		"desc": "something",
	}}

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, item))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, "my-slo", got["name"])
	assert.NotContains(t, got, "uuid")
	assert.NotContains(t, got, "desc")
}

func TestEncode_JSONFields_MapValue(t *testing.T) {
	opts := optsWithJSONFields(t, []string{"name"})

	value := map[string]any{
		"name":   "my-resource",
		"status": "active",
		"count":  42.0,
	}

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, value))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, map[string]any{"name": "my-resource"}, got)
}

func TestEncode_JSONFields_UnstructuredList(t *testing.T) {
	opts := optsWithJSONFields(t, []string{"name", "target"})

	list := unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			{Object: map[string]any{"name": "check-1", "target": "https://a.com", "freq": 60000.0}},
			{Object: map[string]any{"name": "check-2", "target": "https://b.com", "freq": 30000.0}},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, list))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

	rawItems, ok := got["items"]
	require.True(t, ok)
	items, ok := rawItems.([]any)
	require.True(t, ok)
	require.Len(t, items, 2)

	first, ok := items[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "check-1", first["name"])
	assert.Equal(t, "https://a.com", first["target"])
	assert.NotContains(t, first, "freq")
}

func TestEncode_JSONFields_ListEnvelopePreservesPaginationMetadata(t *testing.T) {
	type listEnvelope struct {
		Metadata map[string]any   `json:"metadata"`
		Items    []map[string]any `json:"items"`
	}

	opts := optsWithJSONFields(t, []string{"name"})
	value := listEnvelope{
		Metadata: map[string]any{
			"continue":           "next-page",
			"resourceVersion":    "rv-1",
			"remainingItemCount": 12,
		},
		Items: []map[string]any{
			{"name": "dash-1", "title": "Dashboard 1"},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, value))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	metadata, ok := got["metadata"].(map[string]any)
	require.True(t, ok, "expected pagination metadata in output: %s", buf.String())
	assert.Equal(t, "next-page", metadata["continue"])
	assert.Equal(t, "rv-1", metadata["resourceVersion"])
	assert.InDelta(t, 12, metadata["remainingItemCount"], 0)

	items, ok := got["items"].([]any)
	require.True(t, ok, "expected items array in output: %s", buf.String())
	require.Len(t, items, 1)
	first, ok := items[0].(map[string]any)
	require.True(t, ok, "expected first item object in output: %s", buf.String())
	assert.Equal(t, "dash-1", first["name"])
	assert.NotContains(t, first, "title")
}

func TestEncode_JSONFields_ArbitrarySlice(t *testing.T) {
	// Simulates provider commands that pass []SomeStruct to Encode.
	// Slices must be encoded as a JSON array [...], not {"items":[...]} (C.3b).
	type item struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Extra  int    `json:"extra"`
	}

	opts := optsWithJSONFields(t, []string{"name"})

	value := []item{
		{Name: "slo-1", Status: "active", Extra: 1},
		{Name: "slo-2", Status: "inactive", Extra: 2},
	}

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, value))

	var got []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got),
		"slice input should produce a JSON array [...], not {\"items\":[...]}; got: %s", buf.String())
	require.Len(t, got, 2)

	assert.Equal(t, "slo-1", got[0]["name"])
	assert.NotContains(t, got[0], "status")
	assert.NotContains(t, got[0], "extra")
}

func TestEncode_JSONFields_SingleKeyEnvelope(t *testing.T) {
	// Provider list commands wrap their items under a single non-"items" key
	// (e.g. {"datasources": [...]}). Field selection must descend into the
	// array and preserve the wrapper key.
	type item struct {
		UID  string `json:"uid"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	type envelope struct {
		Datasources []item `json:"datasources"`
	}

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name: "populated envelope selects per item and preserves wrapper key",
			value: &envelope{Datasources: []item{
				{UID: "a", Name: "prom", Type: "prometheus"},
				{UID: "b", Name: "logs", Type: "loki"},
			}},
			want: `{"datasources":[{"uid":"a","name":"prom"},{"uid":"b","name":"logs"}]}`,
		},
		{
			name:  "empty envelope preserves wrapper key with empty array",
			value: &envelope{Datasources: []item{}},
			want:  `{"datasources":[]}`,
		},
		{
			name: "single-key scalar array is not an envelope",
			value: struct {
				Tags []string `json:"tags"`
			}{Tags: []string{"a", "b"}},
			want: `{"uid":null,"name":null}`,
		},
		{
			name: "single-key object value is not an envelope",
			value: struct {
				Spec map[string]any `json:"spec"`
			}{Spec: map[string]any{"uid": "x"}},
			want: `{"uid":null,"name":null}`,
		},
		{
			name: "multi-key object gets flat extraction",
			value: struct {
				UID   string `json:"uid"`
				Name  string `json:"name"`
				Other string `json:"other"`
			}{UID: "a", Name: "n", Other: "o"},
			want: `{"uid":"a","name":"n"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := optsWithJSONFields(t, []string{"uid", "name"})
			var buf bytes.Buffer
			require.NoError(t, opts.Encode(&buf, tt.value))
			assert.JSONEq(t, tt.want, buf.String())
		})
	}
}

func TestEncode_JSONDiscovery_SingleKeyEnvelope(t *testing.T) {
	// Discovery on a single-key list envelope must list item-level fields,
	// not the wrapper key.
	type item struct {
		UID  string `json:"uid"`
		Name string `json:"name"`
	}
	type envelope struct {
		Datasources []item `json:"datasources"`
	}

	opts := optsWithJSONDiscovery(t)
	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, &envelope{Datasources: []item{{UID: "a", Name: "x"}}}))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Contains(t, lines, "uid")
	assert.Contains(t, lines, "name")
	assert.NotContains(t, lines, "datasources")
}

func TestEncode_JSONDiscovery_PrintsFieldNames(t *testing.T) {
	opts := optsWithJSONDiscovery(t)

	// Use map[string]any — the type providers commonly pass to Encode.
	// (Unstructured value types lack the pointer-receiver MarshalJSON.)
	value := map[string]any{
		"name": "my-slo",
		"uuid": "abc-123",
		"spec": map[string]any{
			"title": "My SLO",
		},
	}

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, value))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	// Should contain field names, not JSON
	assert.Contains(t, lines, "name")
	assert.Contains(t, lines, "uuid")
	assert.Contains(t, lines, "spec")
	assert.Contains(t, lines, "spec.title")

	// Must NOT be valid JSON (it's a field list, not encoded data)
	var decoded any
	assert.Error(t, json.Unmarshal(buf.Bytes(), &decoded), "discovery output should not be JSON")
}

func TestEncode_JSONDiscovery_SliceInput(t *testing.T) {
	// Discovery on a slice should use the first element's fields.
	type item struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	opts := optsWithJSONDiscovery(t)
	value := []item{{Name: "a", Status: "ok"}, {Name: "b", Status: "err"}}

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, value))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Contains(t, lines, "name")
	assert.Contains(t, lines, "status")
}

func TestEncode_NonJSONCodec_JSONFieldsIgnored(t *testing.T) {
	// When output is YAML, JSONFields must NOT interfere.
	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Set("output", "yaml"))

	// Manually set JSONFields (simulating a scenario where someone sets it
	// without going through applyJSONFlag — defensive test).
	opts.JSONFields = []string{"name"}

	item := map[string]any{
		"name": "my-resource",
		"kind": "Dashboard",
	}

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, item))

	// Output should contain ALL fields (YAML, not filtered).
	output := buf.String()
	assert.Contains(t, output, "kind")
	assert.Contains(t, output, "name")
}

func TestFieldSelectCodec_SliceOfUnstructured(t *testing.T) {
	// Directly tests FieldSelectCodec.Encode with []unstructured.Unstructured.
	// Plain Go slices must be encoded as a JSON array [...] not {"items":[...]} (C.3b).
	codec := cmdio.NewFieldSelectCodec([]string{"name"})

	value := []unstructured.Unstructured{
		{Object: map[string]any{"name": "a", "kind": "Dashboard"}},
		{Object: map[string]any{"name": "b", "kind": "Folder"}},
	}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, value))

	var got []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got),
		"slice input must produce a JSON array [...], not {\"items\":[...]}; got: %s", buf.String())
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0]["name"])
	assert.NotContains(t, got[0], "kind")
}
