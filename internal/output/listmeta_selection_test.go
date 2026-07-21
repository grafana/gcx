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
