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

func TestAgentsCodec_BelowThreshold(t *testing.T) {
	codec := cmdio.NewAgentsCodecForTesting()

	data := []map[string]any{
		{"name": "alpha", "kind": "Dashboard"},
		{"name": "beta", "kind": "Dashboard"},
	}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	var got []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, data, got)
}

func TestAgentsCodec_AboveThreshold_Spills(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "50")
	t.Setenv("TMPDIR", t.TempDir())

	codec := cmdio.NewAgentsCodecForTesting()

	data := []map[string]any{
		{"name": "alpha", "kind": "Dashboard"},
		{"name": "beta", "kind": "Dashboard"},
		{"name": "gamma", "kind": "Dashboard"},
		{"name": "delta", "kind": "Dashboard"},
		{"name": "epsilon", "kind": "Dashboard"},
	}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))

	spillPath, ok := summary["spilled_to"].(string)
	require.True(t, ok, "summary must contain spilled_to")
	assert.True(t, strings.HasPrefix(spillPath, os.Getenv("TMPDIR")), "spill file should be in TMPDIR")

	_, err := os.Stat(spillPath)
	require.NoError(t, err, "spill file must exist")

	spillBytes, err := os.ReadFile(spillPath)
	require.NoError(t, err)
	var full []map[string]any
	require.NoError(t, json.Unmarshal(spillBytes, &full))
	assert.Equal(t, data, full)

	assert.Contains(t, summary, "bytes")
	assert.Contains(t, summary, "total_items")
	assert.EqualValues(t, len(data), summary["total_items"])

	preview, ok := summary["preview_sample"].([]any)
	require.True(t, ok, "preview must be an array")
	assert.LessOrEqual(t, len(preview), 3)
}

func TestAgentsCodec_NonSlice_OmitsItems(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	t.Setenv("TMPDIR", t.TempDir())

	codec := cmdio.NewAgentsCodecForTesting()

	data := map[string]any{"name": "alpha", "kind": "Dashboard"}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))

	assert.NotContains(t, summary, "total_items", "non-slice value should not include total_items count")
	assert.Contains(t, summary, "spilled_to")
}

func TestAgentsCodec_NonSlice_PreviewIsKeyNames(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	t.Setenv("TMPDIR", t.TempDir())

	codec := cmdio.NewAgentsCodecForTesting()

	// Large map that would be expensive to embed verbatim in the spill envelope.
	data := map[string]any{
		"name":    "alpha",
		"payload": strings.Repeat("x", 10_000),
	}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))

	// stdout must be much smaller than the full payload — the preview must
	// not embed the full map values.
	assert.Less(t, buf.Len(), 500, "spill envelope must not embed the full payload")

	// Preview should be the sorted top-level key names, not the full value.
	preview, ok := summary["preview_sample"].([]any)
	require.True(t, ok, "preview for map should be key names as a slice")
	assert.ElementsMatch(t, []any{"name", "payload"}, preview)
}

func TestAgentsCodec_StructWithItems_CountsItems(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	t.Setenv("TMPDIR", t.TempDir())

	type listValue struct {
		Items []map[string]any
	}

	codec := cmdio.NewAgentsCodecForTesting()

	data := listValue{Items: []map[string]any{
		{"name": "alpha"},
		{"name": "beta"},
		{"name": "gamma"},
		{"name": "delta"},
	}}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))

	assert.EqualValues(t, 4, summary["total_items"])

	preview, ok := summary["preview_sample"].([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(preview), 3)
}

func TestAgentsCodec_InvalidEnvVar_FallsBackToDefault(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "not-a-number")

	codec := cmdio.NewAgentsCodecForTesting()

	// Build a payload just under 100 KiB — should NOT spill.
	data := map[string]string{"payload": strings.Repeat("x", 100*1024-200)}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	// Output should be raw JSON, not a spill summary.
	var got map[string]string
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got), "expected plain JSON, not spill summary")
	assert.Equal(t, data, got)
}

func TestAgentsCodec_SpillEnvelope_UsesTotalItems(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	t.Setenv("TMPDIR", t.TempDir())

	codec := cmdio.NewAgentsCodecForTesting()
	data := []map[string]any{{"name": "alpha"}, {"name": "beta"}}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))

	assert.Contains(t, summary, "total_items", "envelope must use total_items (not items) to avoid k8s shape collision")
	assert.NotContains(t, summary, "items", "envelope must not use items — collides with k8s list shape")
}

func TestAgentsCodec_SpillEnvelope_UsesPreviewSample(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	t.Setenv("TMPDIR", t.TempDir())

	codec := cmdio.NewAgentsCodecForTesting()
	data := []map[string]any{{"name": "alpha"}, {"name": "beta"}}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))

	assert.Contains(t, summary, "preview_sample", "envelope must use preview_sample (not preview) to avoid mistaking it for the full dataset")
	assert.NotContains(t, summary, "preview", "envelope must not use preview — too easy to treat as complete data")
}

func TestAgentsCodec_Spill_HasMessageField(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	t.Setenv("TMPDIR", t.TempDir())

	var errBuf bytes.Buffer
	codec := cmdio.NewAgentsCodecWithErrWriter(&errBuf)

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, map[string]any{"name": "alpha"}))

	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))

	msg, ok := summary["message"].(string)
	require.True(t, ok, "spill envelope must contain a string message field")
	spillPath, _ := summary["spilled_to"].(string)
	assert.Contains(t, msg, spillPath, "message must reference the spill file path")
}

func TestAgentsCodec_Spill_EmitsStderrHint(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	t.Setenv("TMPDIR", t.TempDir())

	var errBuf bytes.Buffer
	codec := cmdio.NewAgentsCodecWithErrWriter(&errBuf)

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, map[string]any{"name": "alpha"}))

	hint := errBuf.String()
	require.NotEmpty(t, hint, "spill must emit a hint to errWriter")

	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))
	spillPath, _ := summary["spilled_to"].(string)
	assert.Contains(t, hint, spillPath, "hint must reference the spill file path")
}

func TestAgentsCodec_NoSpill_NoStderrHint(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1000000")

	var errBuf bytes.Buffer
	codec := cmdio.NewAgentsCodecWithErrWriter(&errBuf)

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, map[string]any{"name": "alpha"}))

	assert.Empty(t, errBuf.String(), "no spill means no stderr hint")
}

func TestAgentsCodec_Format(t *testing.T) {
	codec := cmdio.NewAgentsCodecForTesting()
	assert.Equal(t, "agents", string(codec.Format()))
}

func TestAgentsCodec_DecodeReturnsError(t *testing.T) {
	codec := cmdio.NewAgentsCodecForTesting()
	err := codec.Decode(strings.NewReader("{}"), &map[string]any{})
	require.Error(t, err)
}
