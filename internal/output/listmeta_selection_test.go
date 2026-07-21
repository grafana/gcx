package output_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// truncatedEnvelope mirrors the shape list commands emit for a truncated
// page: a single items key plus the reserved list_meta sibling.
type truncatedEnvelope struct {
	Datasources []dsRow         `json:"datasources"`
	ListMeta    *cmdio.ListMeta `json:"list_meta,omitempty"`
}

type dsRow struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func newTruncatedEnvelope() *truncatedEnvelope {
	total := 219
	return &truncatedEnvelope{
		Datasources: []dsRow{{UID: "ds-01", Name: "Datasource 01", Type: "prometheus"}},
		ListMeta: &cmdio.ListMeta{
			Truncated: true,
			Returned:  1,
			Total:     &total,
			Continue:  "gcx datasources list --limit 0",
		},
	}
}

func encodeWithJSONFlag(t *testing.T, jsonFlag string, value any) string {
	t.Helper()
	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	require.NoError(t, flags.Set("json", jsonFlag))
	require.NoError(t, opts.Validate())

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, value))
	return buf.String()
}

// TestFieldSelectionOnTruncatedEnvelope reproduces PR988 defect (b): with a
// list_meta sibling present, `--limit 1 --json uid` must still select from
// the ITEMS ({"datasources":[{"uid":"ds-01"}]}), not treat the envelope as a
// single object and return {"uid": null} — and the truncation signal must
// survive selection.
func TestFieldSelectionOnTruncatedEnvelope(t *testing.T) {
	out := encodeWithJSONFlag(t, "uid", newTruncatedEnvelope())

	var result struct {
		UID         any              `json:"uid"`
		Datasources []map[string]any `json:"datasources"`
		ListMeta    *cmdio.ListMeta  `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result))

	assert.Nil(t, result.UID, "envelope must not be treated as a single object")
	require.Len(t, result.Datasources, 1)
	assert.Equal(t, "ds-01", result.Datasources[0]["uid"], "selection must run per item")

	require.NotNil(t, result.ListMeta, "list_meta must be re-attached after field selection")
	assert.True(t, result.ListMeta.Truncated)
	assert.Equal(t, 1, result.ListMeta.Returned)
	require.NotNil(t, result.ListMeta.Total)
	assert.Equal(t, 219, *result.ListMeta.Total)
}

// TestFieldSelectionOnCompleteEnvelope guards the non-truncated case: no
// list_meta in, none out — absence remains the completeness signal.
func TestFieldSelectionOnCompleteEnvelope(t *testing.T) {
	env := &truncatedEnvelope{Datasources: []dsRow{{UID: "ds-01", Name: "n", Type: "prometheus"}}}
	out := encodeWithJSONFlag(t, "uid", env)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.NotContains(t, result, "list_meta")
	assert.Contains(t, result, "datasources")
}

// TestFieldSelectionOnTruncatedItemsEnvelope covers the k8s-style
// {"items": [...], "list_meta": {...}} shape used by e.g. `irm oncall
// alert-groups list`: item selection keeps the truncation metadata.
func TestFieldSelectionOnTruncatedItemsEnvelope(t *testing.T) {
	type itemsEnvelope struct {
		Items    []dsRow         `json:"items"`
		ListMeta *cmdio.ListMeta `json:"list_meta,omitempty"`
	}
	env := &itemsEnvelope{
		Items:    []dsRow{{UID: "AG1", Name: "g", Type: "t"}},
		ListMeta: &cmdio.ListMeta{Truncated: true, Returned: 1, Continue: "gcx irm oncall alert-groups list --limit 2"},
	}

	out := encodeWithJSONFlag(t, "uid", env)

	var result struct {
		Items    []map[string]any `json:"items"`
		ListMeta *cmdio.ListMeta  `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Items, 1)
	assert.Equal(t, "AG1", result.Items[0]["uid"])
	require.NotNil(t, result.ListMeta, "list_meta must survive selection on items envelopes")
	assert.True(t, result.ListMeta.Truncated)
}

// TestDiscoveryOnTruncatedEnvelope reproduces PR988 defect (c): `--json list`
// on a truncated envelope must discover ITEM fields (uid, name, type), not
// the envelope keys or list_meta.* paths.
func TestDiscoveryOnTruncatedEnvelope(t *testing.T) {
	out := encodeWithJSONFlag(t, "list", newTruncatedEnvelope())

	fields := strings.Fields(out)
	assert.Contains(t, fields, "uid")
	assert.Contains(t, fields, "name")
	assert.Contains(t, fields, "type")
	assert.NotContains(t, fields, "datasources", "wrapper key must not be listed")
	for _, f := range fields {
		assert.False(t, strings.HasPrefix(f, "list_meta"),
			"reserved truncation metadata must be excluded from discovery, got %q", f)
	}
}

// TestDiscoveryOnEmptyEnvelopeWithListMetaField guards the reflection
// fallback: an EMPTY envelope whose struct carries the reserved ListMeta
// field (nil, omitted from JSON) must still discover item fields via the
// sole slice field — the metadata field must not break the single-slice
// shape detection.
func TestDiscoveryOnEmptyEnvelopeWithListMetaField(t *testing.T) {
	out := encodeWithJSONFlag(t, "list", &truncatedEnvelope{Datasources: []dsRow{}})

	fields := strings.Fields(out)
	assert.Contains(t, fields, "uid")
	assert.Contains(t, fields, "name")
	assert.NotContains(t, fields, "datasources")
	assert.NotContains(t, fields, "list_meta")
}

// TestSingleKeyEnvelopeWithUnrelatedSecondKey pins the scoping of the fix:
// only the reserved list_meta key is tolerated. Envelopes with any other
// extra key keep their pre-existing (whole-object selection) behavior — that
// generalization is tracked separately for human review.
func TestSingleKeyEnvelopeWithUnrelatedSecondKey(t *testing.T) {
	codec := cmdio.NewFieldSelectCodec([]string{"uid"})
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, struct {
		Datasources []dsRow        `json:"datasources"`
		Summary     map[string]any `json:"summary"`
	}{
		Datasources: []dsRow{{UID: "ds-01"}},
		Summary:     map[string]any{"count": 1},
	}))

	var result map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	// Two non-reserved keys: not a single-key envelope, so selection applies
	// to the whole object (uid resolves to null) — unchanged from HEAD.
	assert.Contains(t, result, "uid")
	assert.Nil(t, result["uid"])
}

// dynamicEnvelope builds the map-shaped equivalent of a truncated list
// envelope — what a command would produce if it assembled its output as
// map[string]any instead of a typed struct. The reserved list_meta entry is
// the producer's opt-in to the envelope treatment on this fast path.
func dynamicEnvelope(itemsKey string) map[string]any {
	return map[string]any{
		itemsKey: []any{
			map[string]any{"uid": "ds-01", "name": "Datasource 01", "type": "prometheus"},
		},
		"list_meta": map[string]any{
			"truncated": true,
			"returned":  float64(1),
		},
	}
}

// TestFieldSelectionOnDynamicMapItemsEnvelope covers the direct map[string]any
// fast path in the codec's type switch, which bypasses the marshal step: an
// items-keyed map with a reserved list_meta entry must select per item and
// re-attach the metadata, exactly like the typed-struct path.
func TestFieldSelectionOnDynamicMapItemsEnvelope(t *testing.T) {
	out := encodeWithJSONFlag(t, "uid", dynamicEnvelope("items"))

	var result struct {
		UID      any              `json:"uid"`
		Items    []map[string]any `json:"items"`
		ListMeta *cmdio.ListMeta  `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Nil(t, result.UID, "envelope must not be treated as a single object")
	require.Len(t, result.Items, 1)
	assert.Equal(t, "ds-01", result.Items[0]["uid"])
	require.NotNil(t, result.ListMeta, "list_meta must survive selection on dynamic map envelopes")
	assert.True(t, result.ListMeta.Truncated)
}

// TestFieldSelectionOnDynamicMapSingleKeyEnvelope is the provider-envelope
// variant of the direct-map fast path.
func TestFieldSelectionOnDynamicMapSingleKeyEnvelope(t *testing.T) {
	out := encodeWithJSONFlag(t, "uid", dynamicEnvelope("datasources"))

	var result struct {
		UID         any              `json:"uid"`
		Datasources []map[string]any `json:"datasources"`
		ListMeta    *cmdio.ListMeta  `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Nil(t, result.UID)
	require.Len(t, result.Datasources, 1)
	assert.Equal(t, "ds-01", result.Datasources[0]["uid"])
	require.NotNil(t, result.ListMeta)
	assert.True(t, result.ListMeta.Truncated)
}

// TestFieldSelectionOnDynamicMapWithoutListMeta pins the pre-reservation
// behavior of the direct-map fast path: WITHOUT the reserved entry, even an
// items-shaped map keeps whole-object selection. Raw passthrough payloads
// (gcx api) must not change behavior because a response happens to be
// items-shaped.
func TestFieldSelectionOnDynamicMapWithoutListMeta(t *testing.T) {
	env := map[string]any{
		"items": []any{map[string]any{"uid": "a"}},
	}
	out := encodeWithJSONFlag(t, "uid", env)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Contains(t, result, "uid")
	assert.Nil(t, result["uid"], "whole-object selection is the pinned behavior for maps without list_meta")
}

// TestFieldSelectionOnDynamicMapNonReservedListMeta: a list_meta key whose
// value is not an object (or null) is genuine payload data, not the reserved
// entry — the map keeps whole-object selection (consistent with
// isListMetaEntry's scoping).
func TestFieldSelectionOnDynamicMapNonReservedListMeta(t *testing.T) {
	env := map[string]any{
		"items":     []any{map[string]any{"uid": "a"}},
		"list_meta": "not-an-object",
	}
	out := encodeWithJSONFlag(t, "uid", env)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Contains(t, result, "uid")
	assert.Nil(t, result["uid"])
}

// TestDiscoveryOnDynamicMapEnvelope covers the direct-map fast path in
// discovery: an envelope map with the reserved entry must discover item
// fields only — never the wrapper key or list_meta.* paths.
func TestDiscoveryOnDynamicMapEnvelope(t *testing.T) {
	for _, key := range []string{"items", "datasources"} {
		out := encodeWithJSONFlag(t, "list", dynamicEnvelope(key))

		fields := strings.Fields(out)
		assert.Contains(t, fields, "uid")
		assert.Contains(t, fields, "name")
		assert.NotContains(t, fields, key, "wrapper key %q must not be listed", key)
		for _, f := range fields {
			assert.False(t, strings.HasPrefix(f, "list_meta"),
				"reserved truncation metadata must be excluded from discovery, got %q", f)
		}
	}
}

// TestDiscoveryOnDynamicMapWithoutListMeta pins the pre-reservation discovery
// behavior for plain maps: their own fields are listed as-is.
func TestDiscoveryOnDynamicMapWithoutListMeta(t *testing.T) {
	out := encodeWithJSONFlag(t, "list", map[string]any{"foo": "bar", "count": float64(2)})

	fields := strings.Fields(out)
	assert.Contains(t, fields, "foo")
	assert.Contains(t, fields, "count")
}

// TestDiscoveryOnEmptyDynamicMapEnvelope pins the empty-envelope decision for
// dynamic maps: unlike a typed struct there is no element type to reflect on,
// so discovery degrades to the envelope's own keys — but the reserved
// list_meta entry must never surface as discoverable fields.
func TestDiscoveryOnEmptyDynamicMapEnvelope(t *testing.T) {
	env := map[string]any{
		"datasources": []any{},
		"list_meta":   map[string]any{"truncated": true, "returned": float64(0)},
	}
	out := encodeWithJSONFlag(t, "list", env)

	fields := strings.Fields(out)
	assert.Contains(t, fields, "datasources", "reduced result: the envelope key is all that is known")
	for _, f := range fields {
		assert.False(t, strings.HasPrefix(f, "list_meta"),
			"reserved truncation metadata must be excluded from discovery, got %q", f)
	}
}

// TestDiscoveryOnDynamicMapNonReservedListMeta: a non-object list_meta value
// is genuine data and stays discoverable — the reservation only claims the
// object-valued shape.
func TestDiscoveryOnDynamicMapNonReservedListMeta(t *testing.T) {
	out := encodeWithJSONFlag(t, "list", map[string]any{
		"items":     []any{map[string]any{"uid": "a"}},
		"list_meta": "not-an-object",
	})

	fields := strings.Fields(out)
	assert.Contains(t, fields, "list_meta", "non-reserved shape keeps pre-reservation discovery")
}

// nativeEnvelope builds a dynamic envelope the way a Go producer naturally
// would — a *ListMeta value and a native []map[string]any items slice, NOT
// the JSON-decoded representation ([]any + map[string]any) that dynamicEnvelope
// uses. Envelope handling must JSON-normalize these before selection.
func nativeEnvelope(itemsKey string) map[string]any {
	return map[string]any{
		itemsKey: []map[string]any{
			{"uid": "ds-01", "name": "Datasource 01", "type": "prometheus"},
		},
		"list_meta": &cmdio.ListMeta{Truncated: true, Returned: 1},
	}
}

// TestFieldSelectionOnNativeMapEnvelope: native Go values (a *ListMeta, a
// []map[string]any slice) must get the same envelope treatment as the
// JSON-decoded representation — the key-presence gate fires before
// normalization, so a native metadata value opts in too.
func TestFieldSelectionOnNativeMapEnvelope(t *testing.T) {
	for _, key := range []string{"items", "datasources"} {
		out := encodeWithJSONFlag(t, "uid", nativeEnvelope(key))

		var result map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &result))
		assert.NotContains(t, result, "uid", "envelope must not be treated as a single object")

		items, ok := result[key].([]any)
		require.True(t, ok, "wrapper key %q must survive selection, got %v", key, result)
		require.Len(t, items, 1, "the native item row must not be dropped")
		row, ok := items[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "ds-01", row["uid"])

		meta, ok := result["list_meta"].(map[string]any)
		require.True(t, ok, "native *ListMeta must be re-attached as the normalized object")
		assert.Equal(t, true, meta["truncated"])
	}
}

// TestFieldSelectionOnHybridMapEnvelope pins the row-dropping regression: a
// JSON-decoded list_meta object alongside a NATIVE []map[string]any items
// slice passed the reserved-key gate but toSliceOfMaps (which only accepts
// []any) returned no rows — selection emitted {"items": [], "list_meta": ...},
// silently discarding data. Normalizing the whole map fixes the slice too.
func TestFieldSelectionOnHybridMapEnvelope(t *testing.T) {
	env := map[string]any{
		"items":     []map[string]any{{"uid": "ds-01"}},
		"list_meta": map[string]any{"truncated": true, "returned": float64(1)},
	}
	out := encodeWithJSONFlag(t, "uid", env)

	var result struct {
		Items    []map[string]any `json:"items"`
		ListMeta *cmdio.ListMeta  `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Items, 1, "native items slice must not be silently dropped")
	assert.Equal(t, "ds-01", result.Items[0]["uid"])
	require.NotNil(t, result.ListMeta)
	assert.True(t, result.ListMeta.Truncated)
}

// TestFieldSelectionOnNativeTypedItemsEnvelope: a typed item slice (what a
// command holding []Row naturally has in hand) is normalized like any other
// native value.
func TestFieldSelectionOnNativeTypedItemsEnvelope(t *testing.T) {
	env := map[string]any{
		"datasources": []dsRow{{UID: "ds-01", Name: "n", Type: "prometheus"}},
		"list_meta":   &cmdio.ListMeta{Truncated: true, Returned: 1},
	}
	out := encodeWithJSONFlag(t, "uid", env)

	var result struct {
		Datasources []map[string]any `json:"datasources"`
		ListMeta    *cmdio.ListMeta  `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Len(t, result.Datasources, 1)
	assert.Equal(t, "ds-01", result.Datasources[0]["uid"])
	require.NotNil(t, result.ListMeta)
}

// TestDiscoveryOnNativeMapEnvelope: discovery must normalize native values
// the same way — item fields listed, never the wrapper key or list_meta.*.
func TestDiscoveryOnNativeMapEnvelope(t *testing.T) {
	for _, key := range []string{"items", "datasources"} {
		out := encodeWithJSONFlag(t, "list", nativeEnvelope(key))

		fields := strings.Fields(out)
		assert.Contains(t, fields, "uid")
		assert.Contains(t, fields, "name")
		assert.NotContains(t, fields, key, "wrapper key %q must not be listed", key)
		for _, f := range fields {
			assert.False(t, strings.HasPrefix(f, "list_meta"),
				"reserved truncation metadata must be excluded from discovery, got %q", f)
		}
	}
}
