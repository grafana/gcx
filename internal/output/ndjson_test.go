package output_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeLines splits NDJSON output into trimmed non-empty lines and asserts each
// is independently parseable JSON, returning the decoded objects.
func decodeLines(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimRight(string(b), "\n"), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		require.NoErrorf(t, json.Unmarshal([]byte(line), &obj), "line not valid JSON: %s", line)
		out = append(out, obj)
	}
	return out
}

func TestNDJSONCodec_SingleObject_OneLine(t *testing.T) {
	codec := cmdio.NewNDJSONCodecForTesting()

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, map[string]any{"uid": "abc", "name": "prom"}))

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 1)
	assert.Equal(t, "result", lines[0]["kind"])
	assert.Equal(t, map[string]any{"uid": "abc", "name": "prom"}, lines[0]["data"])
}

func TestNDJSONCodec_Slice_OneLinePerElement(t *testing.T) {
	codec := cmdio.NewNDJSONCodecForTesting()

	data := []map[string]any{
		{"uid": "a"}, {"uid": "b"}, {"uid": "c"},
	}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 3)
	for i, want := range []string{"a", "b", "c"} {
		assert.Equal(t, "result", lines[i]["kind"])
		assert.Equal(t, map[string]any{"uid": want}, lines[i]["data"])
	}
}

func TestNDJSONCodec_StructWithItems_OneLinePerItem(t *testing.T) {
	codec := cmdio.NewNDJSONCodecForTesting()

	type listValue struct {
		Items []map[string]any
	}
	data := listValue{Items: []map[string]any{{"name": "alpha"}, {"name": "beta"}}}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 2)
	assert.Equal(t, map[string]any{"name": "alpha"}, lines[0]["data"])
	assert.Equal(t, map[string]any{"name": "beta"}, lines[1]["data"])
}

func TestNDJSONCodec_WrapperMap_NotUnwrapped(t *testing.T) {
	// A single-key wrapper map (the shape list commands pass for non-table
	// formats) is NOT a collection — it emits as one line. The footgun is still
	// fixed because the merged stream stays uniform NDJSON.
	codec := cmdio.NewNDJSONCodecForTesting()

	data := map[string]any{"datasources": []any{
		map[string]any{"uid": "a"},
		map[string]any{"uid": "b"},
	}}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 1)
	assert.Equal(t, "result", lines[0]["kind"])
	assert.Contains(t, lines[0]["data"], "datasources")
}

func TestNDJSONCodec_EmptySlice_EmptySentinel(t *testing.T) {
	codec := cmdio.NewNDJSONCodecForTesting()

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, []map[string]any{}))

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 1, "empty collection emits a single sentinel line")
	assert.Equal(t, "empty", lines[0]["kind"])
	assert.NotContains(t, lines[0], "data", "sentinel carries no data field")
}

func TestNDJSONCodec_EmptyStructWithItems_EmptySentinel(t *testing.T) {
	codec := cmdio.NewNDJSONCodecForTesting()

	type listValue struct {
		Items []map[string]any `json:"items"`
	}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, listValue{Items: []map[string]any{}}))

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 1)
	assert.Equal(t, "empty", lines[0]["kind"])
}

func TestNDJSONCodec_OverThreshold_SpillsOneLine(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	t.Setenv("TMPDIR", t.TempDir())

	var errBuf bytes.Buffer
	codec := cmdio.NewNDJSONCodecWithErrWriter(&errBuf)

	data := []map[string]any{{"name": "alpha"}, {"name": "beta"}, {"name": "gamma"}}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	lines := decodeLines(t, buf.Bytes())
	require.Len(t, lines, 1, "oversized output spills to a single summary line")
	assert.Equal(t, "spill", lines[0]["kind"])
	assert.EqualValues(t, len(data), lines[0]["total_items"])

	spillPath, ok := lines[0]["spilled_to"].(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(spillPath, os.Getenv("TMPDIR")))
	full, err := os.ReadFile(spillPath)
	require.NoError(t, err)
	var got []map[string]any
	require.NoError(t, json.Unmarshal(full, &got))
	assert.Equal(t, data, got)

	assert.NotEmpty(t, errBuf.String(), "spill emits a stderr hint")
}

func TestNDJSONCodec_Format(t *testing.T) {
	codec := cmdio.NewNDJSONCodecForTesting()
	assert.Equal(t, "ndjson", string(codec.Format()))
}

func TestNDJSONCodec_DecodeReturnsError(t *testing.T) {
	codec := cmdio.NewNDJSONCodecForTesting()
	require.Error(t, codec.Decode(strings.NewReader("{}"), &map[string]any{}))
}
