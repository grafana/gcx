package investigations_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatThreadTextCodec_Encode(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{
			ID:   "m1",
			Role: "assistant",
			Content: []investigations.ChatContentBlock{
				{Type: "text", Text: "p99 latency spiked."},
				{Type: "tool_use", ID: "tu_1", Name: "search_skills", Input: json.RawMessage(`{"query":"api latency"}`)},
			},
		},
		{
			ID:   "m2",
			Role: "tool",
			Content: []investigations.ChatContentBlock{
				{Type: "tool_result", ToolUseID: "tu_1", DurationMs: 42, ToolResult: json.RawMessage(`{"ok":true}`)},
			},
		},
	}

	t.Run("table", func(t *testing.T) {
		var buf bytes.Buffer
		codec := &investigations.ChatThreadTextCodec{}
		require.NoError(t, codec.Encode(&buf, messages))
		out := buf.String()
		assert.Contains(t, out, "[assistant]")
		assert.Contains(t, out, "p99 latency spiked.")
		assert.Contains(t, out, "tool_use search_skills")
		assert.Contains(t, out, "tool_result")
		assert.Contains(t, out, "durationMs=42")
	})

	t.Run("wide includes IDs", func(t *testing.T) {
		var buf bytes.Buffer
		codec := &investigations.ChatThreadTextCodec{Wide: true}
		require.NoError(t, codec.Encode(&buf, messages))
		out := buf.String()
		assert.Contains(t, out, "id=m1")
		assert.Contains(t, out, "for=tu_1")
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &investigations.ChatThreadTextCodec{}
		require.Error(t, codec.Encode(&bytes.Buffer{}, "wrong"))
	})
}

func TestChatThreadTextCodec_ErrorMarker(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{Role: "tool", Content: []investigations.ChatContentBlock{
			{Type: "tool_result", ToolUseID: "tu_1", IsError: true, ToolResult: json.RawMessage(`{"error":"boom"}`)},
		}},
	}
	var buf bytes.Buffer
	codec := &investigations.ChatThreadTextCodec{}
	require.NoError(t, codec.Encode(&buf, messages))
	assert.Contains(t, buf.String(), "✗")
}

func TestNarrativeCodec_Encode(t *testing.T) {
	var buf bytes.Buffer
	codec := investigations.NarrativeCodec{}
	require.NoError(t, codec.Encode(&buf, "p99 latency spiked at 14:02."))
	assert.Equal(t, "p99 latency spiked at 14:02.\n", buf.String())
}

func TestNarrativeCodec_EmptyString(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, investigations.NarrativeCodec{}.Encode(&buf, ""))
	assert.Empty(t, buf.String())
}

func TestToolsTableCodec_Encode(t *testing.T) {
	calls := []investigations.ToolCall{
		{ID: "tu_1", Name: "search_skills", DurationMs: 42, Input: json.RawMessage(`{"query":"q"}`), Result: json.RawMessage(`{"ok":true}`)},
		{ID: "tu_2", Name: "prometheus_query_handler", IsError: true, Result: json.RawMessage(`{"error":"x"}`)},
		{ID: "tu_3", Name: "loki_query_handler_investigator"},
	}

	t.Run("table shows status", func(t *testing.T) {
		var buf bytes.Buffer
		codec := &investigations.ToolsTableCodec{}
		require.NoError(t, codec.Encode(&buf, calls))
		out := buf.String()
		assert.Contains(t, out, "search_skills")
		assert.Contains(t, out, "ok")
		assert.Contains(t, out, "error")
		assert.Contains(t, out, "pending")
		assert.Contains(t, out, "42")
	})

	t.Run("wide includes ID", func(t *testing.T) {
		var buf bytes.Buffer
		codec := &investigations.ToolsTableCodec{Wide: true}
		require.NoError(t, codec.Encode(&buf, calls))
		assert.Contains(t, buf.String(), "tu_1")
	})

	t.Run("wrong type", func(t *testing.T) {
		require.Error(t, (&investigations.ToolsTableCodec{}).Encode(&bytes.Buffer{}, "wrong"))
	})
}

func TestSkillsTableCodec_Encode(t *testing.T) {
	matches := []investigations.SkillMatch{
		{ToolUseID: "tu_1", Query: "api latency", SkillID: "s1", SkillName: "APILatency", Score: 0.913, Chunk: "check p99\nlatency", Source: "skills/api.md"},
		{ToolUseID: "tu_1", Query: "api latency", SkillID: "s2", SkillName: "", Score: 0, Chunk: ""},
	}

	t.Run("table", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, (&investigations.SkillsTableCodec{}).Encode(&buf, matches))
		out := buf.String()
		assert.Contains(t, out, "APILatency")
		assert.Contains(t, out, "0.913")
		assert.Contains(t, out, "check p99 latency")
		assert.Contains(t, out, "s2") // empty name falls back to ID
	})

	t.Run("wide adds source column", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, (&investigations.SkillsTableCodec{Wide: true}).Encode(&buf, matches))
		out := buf.String()
		assert.Contains(t, out, "SOURCE")
		assert.Contains(t, out, "skills/api.md")
	})
}
