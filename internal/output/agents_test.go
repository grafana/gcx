package output_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setSpillTempDir(t *testing.T, dir string) {
	t.Helper()
	setSpillTempDir(t, dir)
	t.Setenv("TMP", dir)
	t.Setenv("TEMP", dir)
}

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
	setSpillTempDir(t, t.TempDir())

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

	// The receipt shape differs from the domain result, so it must carry
	// collision-resistant discriminators a consumer can dispatch on.
	assert.Equal(t, cmdio.SpillReferenceType, summary["type"])
	assert.Equal(t, "1", summary["schema_version"])
	assert.Equal(t, "json", summary["content_format"])
}

// TestAgentsCodec_BelowThreshold_NoDiscriminator pins the other side of the
// threshold: an in-line payload is the domain result itself and must NOT be
// wrapped or tagged.
func TestAgentsCodec_BelowThreshold_NoDiscriminator(t *testing.T) {
	codec := cmdio.NewAgentsCodecForTesting()

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, []map[string]any{{"name": "alpha"}}))

	var value []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &value))
	assert.Equal(t, []map[string]any{{"name": "alpha"}}, value)
}

func TestAgentsCodec_Spill_ListEnvelope(t *testing.T) {
	// A ListEnvelope spill must preview and count the items under the
	// declared key, not treat the envelope as an opaque struct.
	t.Setenv("GCX_AGENT_SPILL_BYTES", "50")
	setSpillTempDir(t, t.TempDir())

	codec := cmdio.NewAgentsCodecForTesting()

	value := &listEnvelopeResult{
		Investigations: []listEnvelopeItem{
			{UID: "a", Name: "one", Type: "x"},
			{UID: "b", Name: "two", Type: "y"},
			{UID: "c", Name: "three", Type: "z"},
			{UID: "d", Name: "four", Type: "w"},
		},
		Total: 42,
	}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, value))

	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))

	require.Contains(t, summary, "spilled_to")
	assert.EqualValues(t, len(value.Investigations), summary["total_items"],
		"total_items must count envelope items, not envelope keys")

	preview, ok := summary["preview_sample"].([]any)
	require.True(t, ok, "preview must be the item array, got %T", summary["preview_sample"])
	require.Len(t, preview, 3)
	first, ok := preview[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "a", first["uid"])
}

func TestAgentsCodec_NonSlice_OmitsItems(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	setSpillTempDir(t, t.TempDir())

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
	setSpillTempDir(t, t.TempDir())

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

	// stdout must be much smaller than the full 10KB payload — the preview
	// must not embed the full map values. The cap allows the fixed receipt
	// fields (type/schema_version/content_format discriminators, path,
	// message, hint) but nothing payload-proportional.
	assert.Less(t, buf.Len(), 900, "spill envelope must not embed the full payload")

	// Preview should be the sorted top-level key names, not the full value.
	preview, ok := summary["preview_sample"].([]any)
	require.True(t, ok, "preview for map should be key names as a slice")
	assert.ElementsMatch(t, []any{"name", "payload"}, preview)
}

func TestAgentsCodec_StructWithItems_CountsItems(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	setSpillTempDir(t, t.TempDir())

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

func TestAgentsCodec_ZeroThresholdDisablesSpill(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "0")
	dir := t.TempDir()
	setSpillTempDir(t, dir)

	codec := cmdio.NewAgentsCodecForTesting()
	data := map[string]string{"payload": strings.Repeat("x", 512*1024)}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, data))

	var got map[string]string
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got), "stdout carries the full payload, not a spill receipt")
	assert.Equal(t, data, got)
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, files, "no spill file")
	assert.Equal(t, 0, cmdio.SpillThreshold())
}

// TestAgentsCodec_Threshold pins the spill threshold: the 24 KiB default,
// the GCX_AGENT_SPILL_BYTES override, and the fallback for a bad value.
func TestAgentsCodec_Threshold(t *testing.T) {
	// An encoded string value is len+3 bytes: two quotes and a newline.
	const defaultBytes = 24 * 1024
	tests := []struct {
		name      string
		env       string
		payload   int
		wantSpill bool
	}{
		{name: "default: at threshold stays inline", payload: defaultBytes - 3},
		{name: "default: above threshold spills", payload: defaultBytes - 2, wantSpill: true},
		{name: "default: old 100 KiB size spills", payload: 60 * 1024, wantSpill: true},
		{name: "override: larger threshold keeps inline", env: "204800", payload: 60 * 1024},
		{name: "override: smaller threshold spills", env: "100", payload: 98, wantSpill: true},
		{name: "invalid value falls back to default", env: "not-a-number", payload: defaultBytes - 3},
		{name: "zero disables spilling", env: "0", payload: 60 * 1024},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GCX_AGENT_SPILL_BYTES", tt.env)
			dir := t.TempDir()
			setSpillTempDir(t, dir)

			value := strings.Repeat("x", tt.payload)
			var buf bytes.Buffer
			require.NoError(t, cmdio.NewAgentsCodecWithErrWriter(&bytes.Buffer{}).Encode(&buf, value))

			files, err := os.ReadDir(dir)
			require.NoError(t, err)
			if !tt.wantSpill {
				assert.Empty(t, files)
				assert.Equal(t, `"`+value+"\"\n", buf.String())
				return
			}
			require.Len(t, files, 1)
			var summary map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))
			assert.Equal(t, cmdio.SpillReferenceType, summary["type"])
		})
	}
}

// TestAgentsCodec_SpillPreview pins the receipt preview: identifying fields
// only, a byte cap, and total_items for every list shape.
func TestAgentsCodec_SpillPreview(t *testing.T) {
	big := strings.Repeat("x", 4096)
	tests := []struct {
		name        string
		value       any
		wantPreview any // nil means preview_sample is JSON null
		wantTotal   any // nil means total_items is absent
	}{
		{
			name: "projects identifying fields",
			value: []map[string]any{
				{"uid": "a", "title": "A", "panels": big},
				{"uid": "b", "title": "B", "panels": big},
				{"uid": "c", "title": "C", "panels": big},
				{"uid": "d", "title": "D", "panels": big},
			},
			wantPreview: []any{
				map[string]any{"uid": "a", "title": "A"},
				map[string]any{"uid": "b", "title": "B"},
				map[string]any{"uid": "c", "title": "C"},
			},
			wantTotal: 4.0,
		},
		{
			name: "projects nested k8s fields",
			value: map[string]any{
				"apiVersion": "v1",
				"items": []any{
					map[string]any{"metadata": map[string]any{"name": "d1", "labels": big}, "spec": map[string]any{"title": "Dash 1", "panels": big}},
				},
			},
			wantPreview: []any{map[string]any{"metadata.name": "d1", "spec.title": "Dash 1"}},
			wantTotal:   1.0,
		},
		{
			name:        "single-key envelope counts items",
			value:       map[string]any{"datasources": []any{map[string]any{"name": "prom", "jsonData": big}, map[string]any{"name": "loki", "jsonData": big}}},
			wantPreview: []any{map[string]any{"name": "prom"}, map[string]any{"name": "loki"}},
			wantTotal:   2.0,
		},
		{
			name:        "single-key envelope with list_meta counts items",
			value:       map[string]any{"datasources": []any{map[string]any{"id": 7, "blob": big}}, "list_meta": map[string]any{"truncated": true}},
			wantPreview: []any{map[string]any{"id": 7.0}},
			wantTotal:   1.0,
		},
		{
			name:        "no identifying field keeps scalar fields",
			value:       []map[string]any{{"state": "firing", "labels": map[string]any{"a": big}}},
			wantPreview: []any{map[string]any{"state": "firing"}},
			wantTotal:   1.0,
		},
		{
			name: "drops items that do not fit",
			value: []map[string]any{
				{"name": strings.Repeat("a", 900)},
				{"name": strings.Repeat("b", 900)},
				{"name": strings.Repeat("c", 900)},
			},
			wantPreview: []any{
				map[string]any{"name": strings.Repeat("a", 900)},
				map[string]any{"name": strings.Repeat("b", 900)},
			},
			wantTotal: 3.0,
		},
		{
			name:        "omits preview when one item does not fit",
			value:       []map[string]any{{"name": big}, {"name": "small"}},
			wantPreview: nil,
			wantTotal:   2.0,
		},
		{
			name:        "huge item with no scalar field is omitted",
			value:       []any{map[string]any{"rules": []any{big, big}}},
			wantPreview: nil,
			wantTotal:   1.0,
		},
		{
			name:        "scalar items are kept",
			value:       []string{"a", "b", "c", "d", big},
			wantPreview: []any{"a", "b", "c"},
			wantTotal:   5.0,
		},
		{
			name:        "object preview is sorted key names",
			value:       map[string]any{"zeta": big, "alpha": 1},
			wantPreview: []any{"alpha", "zeta"},
		},
		{
			name:        "empty list has empty preview",
			value:       map[string]any{"items": []any{}, "metadata": map[string]any{"blob": big}},
			wantPreview: []any{},
			wantTotal:   0.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
			setSpillTempDir(t, t.TempDir())

			var buf bytes.Buffer
			require.NoError(t, cmdio.NewAgentsCodecWithErrWriter(&bytes.Buffer{}).Encode(&buf, tt.value))

			var summary map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))

			require.Contains(t, summary, "preview_sample")
			assert.Equal(t, tt.wantPreview, summary["preview_sample"])
			preview, err := json.Marshal(summary["preview_sample"])
			require.NoError(t, err)
			assert.LessOrEqual(t, len(preview), 2048, "preview must fit the byte cap")

			if tt.wantTotal == nil {
				assert.NotContains(t, summary, "total_items")
			} else {
				assert.Equal(t, tt.wantTotal, summary["total_items"])
			}

			hint, _ := summary["hint"].(string)
			assert.Contains(t, hint, "--json <fields>")
			assert.Contains(t, hint, "--json list")
		})
	}
}

// TestAgentsCodec_Spill_ReceiptIsBounded pins the receipt size for items that
// are very large (for example alert rule groups): the receipt must stay far
// below the spill threshold.
func TestAgentsCodec_Spill_ReceiptIsBounded(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "")
	setSpillTempDir(t, t.TempDir())

	groups := make([]map[string]any, 50)
	for i := range groups {
		groups[i] = map[string]any{
			"name":  "group",
			"rules": []any{map[string]any{"expr": strings.Repeat("x", 60*1024)}},
		}
	}

	var buf bytes.Buffer
	require.NoError(t, cmdio.NewAgentsCodecWithErrWriter(&bytes.Buffer{}).Encode(&buf, groups))

	assert.Less(t, buf.Len(), 4096, "receipt must be bounded")
	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))
	assert.EqualValues(t, 50, summary["total_items"])
	assert.Equal(t, []any{
		map[string]any{"name": "group"},
		map[string]any{"name": "group"},
		map[string]any{"name": "group"},
	}, summary["preview_sample"])
}

func TestAgentsCodec_SpillEnvelope_UsesTotalItems(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	setSpillTempDir(t, t.TempDir())

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
	setSpillTempDir(t, t.TempDir())

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
	setSpillTempDir(t, t.TempDir())

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
	setSpillTempDir(t, t.TempDir())

	// TTY mode: the hint stays a plain "hint: ..." line.
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	var errBuf bytes.Buffer
	codec := cmdio.NewAgentsCodecWithErrWriter(&errBuf)

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, map[string]any{"name": "alpha"}))

	hint := errBuf.String()
	require.NotEmpty(t, hint, "spill must emit a hint to errWriter")
	assert.True(t, strings.HasPrefix(hint, "hint: "), "TTY-mode hint must keep the hint: prefix, got %q", hint)

	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))
	spillPath, _ := summary["spilled_to"].(string)
	assert.Contains(t, hint, spillPath, "hint must reference the spill file path")
}

// TestAgentsCodec_Spill_AgentModeHintIsJSONL pins the stderr contract in agent
// mode: diagnostics must be JSONL records with a typed class (FR-104), never
// raw prose — a bare "hint: ..." line would break agent stderr parsing.
func TestAgentsCodec_Spill_AgentModeHintIsJSONL(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "1")
	setSpillTempDir(t, t.TempDir())

	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	var errBuf bytes.Buffer
	codec := cmdio.NewAgentsCodecWithErrWriter(&errBuf)

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, map[string]any{"name": "alpha"}))

	line := strings.TrimSpace(errBuf.String())
	require.NotEmpty(t, line, "spill must emit a hint to errWriter")

	var event map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &event), "agent-mode hint must be a JSONL record, got %q", line)
	assert.Equal(t, "hint", event["class"])

	var summary map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &summary))
	spillPath, _ := summary["spilled_to"].(string)
	sum, _ := event["summary"].(string)
	assert.Contains(t, sum, spillPath, "hint summary must reference the spill file path")
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

// oversizedRecords is a payload comfortably above the 50-byte threshold the
// redirect tests set, so the only thing deciding spill vs inline is the
// destination writer.
func oversizedRecords() []map[string]any {
	return []map[string]any{
		{"name": "alpha", "kind": "Dashboard"},
		{"name": "beta", "kind": "Dashboard"},
		{"name": "gamma", "kind": "Dashboard"},
		{"name": "delta", "kind": "Dashboard"},
		{"name": "epsilon", "kind": "Dashboard"},
	}
}

// TestAgentsCodec_RegularFileDestination_DoesNotSpill covers `gcx ... > f.json`
// in agent mode: the caller named the destination, so the file must hold the
// data rather than a receipt pointing at a temp file somewhere else.
func TestAgentsCodec_RegularFileDestination_DoesNotSpill(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "50")
	spillDir := t.TempDir()
	setSpillTempDir(t, spillDir)

	dst, err := os.Create(filepath.Join(t.TempDir(), "out.json"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dst.Close() })

	var stderr bytes.Buffer
	codec := cmdio.NewAgentsCodecWithErrWriter(&stderr)
	data := oversizedRecords()
	require.NoError(t, codec.Encode(dst, data))
	require.NoError(t, dst.Close())

	written, err := os.ReadFile(dst.Name())
	require.NoError(t, err)

	var got []map[string]any
	require.NoError(t, json.Unmarshal(written, &got), "the redirect target must hold the payload, not a spill receipt")
	assert.Equal(t, data, got)
	assert.NotContains(t, string(written), cmdio.SpillReferenceType)

	// No receipt means no stderr hint telling the caller to read elsewhere,
	// and no stray temp file left behind.
	assert.Empty(t, stderr.String())
	entries, err := os.ReadDir(spillDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing should be spilled when the destination is a file")
}

// TestAgentsCodec_PipeDestination_StillSpills is the other half of the
// contract: a harness capturing stdout gets a pipe, and its context budget
// stays protected.
func TestAgentsCodec_PipeDestination_StillSpills(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "50")
	setSpillTempDir(t, t.TempDir())

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	read := make(chan []byte, 1)
	go func() {
		out, _ := io.ReadAll(r)
		read <- out
	}()

	codec := cmdio.NewAgentsCodecForTesting()
	require.NoError(t, codec.Encode(w, oversizedRecords()))
	require.NoError(t, w.Close())

	var summary map[string]any
	require.NoError(t, json.Unmarshal(<-read, &summary))
	assert.Equal(t, cmdio.SpillReferenceType, summary["type"])
	assert.NotEmpty(t, summary["spilled_to"])
}
