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
				{Type: "thinking", Thinking: "let me check the database"},
				{Type: "tool_use", ToolID: "tu_1", ToolName: "search_skills", ToolInput: json.RawMessage(`{"queries":[{"query":"latency"}]}`)},
			},
		},
		{
			ID:   "m2",
			Role: "assistant",
			Content: []investigations.ChatContentBlock{
				{
					Type:       "tool_result",
					ToolUseID:  "tu_1",
					ToolName:   "search_skills",
					DurationMs: 42,
					ToolResult: []investigations.ToolResultPart{{Type: "text", Text: "result text"}},
				},
			},
		},
		{
			ID:   "m3",
			Role: "internal",
			Type: "artifact",
			Content: []investigations.ChatContentBlock{
				{Type: "artifact", ArtifactType: "panel", Panel: json.RawMessage(`[{"panelId":"p5"},{"panelId":"p7"}]`)},
			},
		},
	}

	t.Run("table", func(t *testing.T) {
		var buf bytes.Buffer
		codec := &investigations.ChatThreadTextCodec{}
		require.NoError(t, codec.Encode(&buf, messages))
		out := buf.String()
		assert.Contains(t, out, "[assistant]")
		assert.Contains(t, out, "[internal]")
		assert.Contains(t, out, "p99 latency spiked.")
		assert.Contains(t, out, "~ let me check the database")
		assert.Contains(t, out, "tool_use search_skills")
		assert.Contains(t, out, "tool_result")
		assert.Contains(t, out, "search_skills")
		assert.Contains(t, out, "durationMs=42")
		assert.Contains(t, out, "result text")
		assert.Contains(t, out, "◆ artifact panel")
		assert.Contains(t, out, "p5,p7")
	})

	t.Run("wide includes IDs", func(t *testing.T) {
		var buf bytes.Buffer
		codec := &investigations.ChatThreadTextCodec{Wide: true}
		require.NoError(t, codec.Encode(&buf, messages))
		out := buf.String()
		assert.Contains(t, out, "id=m1")
		assert.Contains(t, out, "for=tu_1")
		assert.Contains(t, out, "id=tu_1")
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &investigations.ChatThreadTextCodec{}
		require.Error(t, codec.Encode(&bytes.Buffer{}, "wrong"))
	})
}

func TestChatThreadTextCodec_ErrorMarker(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{
				Type:       "tool_result",
				ToolUseID:  "tu_1",
				IsError:    true,
				ToolResult: []investigations.ToolResultPart{{Type: "text", Text: "boom"}},
			},
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

func TestNarrativeCodec_Format(t *testing.T) {
	assert.Equal(t, "table", string(investigations.NarrativeCodec{}.Format()))
	assert.Equal(t, "agents", string(investigations.NarrativeCodec{Format_: "agents"}.Format()))
}

func TestToolsTableCodec_Encode(t *testing.T) {
	calls := []investigations.ToolCall{
		{
			ID:         "tu_1",
			Name:       "search_skills",
			DurationMs: 42,
			Input:      json.RawMessage(`{"queries":[{"query":"q"}]}`),
			Result:     []investigations.ToolResultPart{{Type: "text", Text: "ok"}},
		},
		{
			ID:      "tu_2",
			Name:    "prometheus_query_handler",
			IsError: true,
			Result:  []investigations.ToolResultPart{{Type: "text", Text: "x"}},
		},
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
