package investigations_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures in this file mirror the real Grafana Assistant plugin
// response from `/chats/{chatId}/all-messages` — captured live against a v2
// investigation. See the PR comment for the recorded shape.

func TestGetChatThread(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/chats/chat-1/all-messages")
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"messages": []map[string]any{
					{
						"id":   "m1",
						"role": "assistant",
						"type": "message",
						"content": []map[string]any{
							{"type": "text", "text": "Looking at the API latency."},
							{
								"type":      "tool_use",
								"toolId":    "tu_1",
								"toolName":  "search_skills",
								"toolInput": map[string]any{"queries": []map[string]any{{"keywords": "api latency", "query": "api latency runbook"}}},
							},
						},
					},
					{
						"id":   "m2",
						"role": "assistant",
						"type": "message",
						"content": []map[string]any{
							{
								"type":       "tool_result",
								"toolUseId":  "tu_1",
								"toolName":   "search_skills",
								"durationMs": 42,
								"toolResult": []map[string]any{{"type": "text", "text": "<results count=\"1\"><chunk skill_id=\"s1\" title=\"API latency runbook\">check p99</chunk></results>"}},
							},
						},
					},
				},
			},
		})
	}))

	messages, err := client.GetChatThread(t.Context(), "chat-1")
	require.NoError(t, err)
	require.Len(t, messages, 2)
	assert.Equal(t, "assistant", messages[0].Role)
	assert.Len(t, messages[0].Content, 2)
	assert.Equal(t, "tool_use", messages[0].Content[1].Type)
	assert.Equal(t, "search_skills", messages[0].Content[1].ToolName)
	assert.Equal(t, "tu_1", messages[0].Content[1].ToolID)
	assert.NotEmpty(t, messages[0].Content[1].ToolInput)
	assert.Equal(t, "tool_result", messages[1].Content[0].Type)
	assert.Equal(t, "tu_1", messages[1].Content[0].ToolUseID)
	assert.Equal(t, int64(42), messages[1].Content[0].DurationMs)
	require.Len(t, messages[1].Content[0].ToolResult, 1)
	assert.Equal(t, "text", messages[1].Content[0].ToolResult[0].Type)
}

func TestGetChatThread_EmptyMessages(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"data": map[string]any{}})
	}))
	messages, err := client.GetChatThread(t.Context(), "chat-1")
	require.NoError(t, err)
	assert.Empty(t, messages)
}

func TestGetChatThread_ServerError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	_, err := client.GetChatThread(t.Context(), "chat-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestNarrative(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{Role: "user", Content: []investigations.ChatContentBlock{{Type: "text", Text: "what's wrong?"}}},
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{Type: "text", Text: "<context>internal</context>p99 spiked at 14:02"},
			{Type: "thinking", Thinking: "let me check metrics"},
			{Type: "tool_use", ToolName: "prometheus_query_handler"},
		}},
		{Role: "assistant", Hidden: true, Content: []investigations.ChatContentBlock{{Type: "text", Text: "hidden note"}}},
		{Role: "assistant", Content: []investigations.ChatContentBlock{{Type: "text", Text: "Likely a slow downstream."}}},
	}
	got := investigations.Narrative(messages)
	assert.Equal(t, "p99 spiked at 14:02\n\nLikely a slow downstream.", got)
}

func TestExtractToolCalls(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{Type: "tool_use", ToolID: "tu_1", ToolName: "search_skills", ToolInput: json.RawMessage(`{"queries":[{"query":"q"}]}`)},
			{Type: "tool_use", ToolID: "tu_2", ToolName: "prometheus_query_handler"},
		}},
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{Type: "tool_result", ToolUseID: "tu_1", ToolName: "search_skills", DurationMs: 12, ToolResult: []investigations.ToolResultPart{{Type: "text", Text: "<results count=\"0\"/>"}}},
			{Type: "tool_result", ToolUseID: "tu_2", ToolName: "prometheus_query_handler", IsError: true, ToolResult: []investigations.ToolResultPart{{Type: "text", Text: "boom"}}},
		}},
	}
	calls := investigations.ExtractToolCalls(messages)
	require.Len(t, calls, 2)
	assert.Equal(t, "search_skills", calls[0].Name)
	assert.Equal(t, "tu_1", calls[0].ID)
	assert.NotEmpty(t, calls[0].Input)
	assert.Equal(t, int64(12), calls[0].DurationMs)
	assert.False(t, calls[0].IsError)
	require.Len(t, calls[0].Result, 1)
	assert.True(t, calls[1].IsError)
}

func TestExtractToolCalls_PendingResult(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{Type: "tool_use", ToolID: "tu_1", ToolName: "loki_query_handler_investigator"},
		}},
	}
	calls := investigations.ExtractToolCalls(messages)
	require.Len(t, calls, 1)
	assert.Empty(t, calls[0].Result)
	assert.Zero(t, calls[0].DurationMs)
}

func TestExtractSkillMatches(t *testing.T) {
	// Real server payload: queries[].query input, XML chunk envelope in toolResult.
	xmlResult := `<results count="2">
<chunk skill_id="78ffce9d-8911-4f8b-8a16-6b42f646ca2d" offset="0" length="294" title="Investigate payment error spikes">Investigate payment error spikes

Confirm symptom, quantify blast radius, inspect logs and traces.</chunk>
<chunk skill_id="78ffce9d-8911-4f8b-8a16-6b42f646ca2d" offset="294" length="200" title="Investigate payment error spikes">Correlate with recent changes &#34;deploy generation&#34;.</chunk>
</results>`

	messages := []investigations.ChatThreadMessage{
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{
				Type:      "tool_use",
				ToolID:    "tu_1",
				ToolName:  "search_skills",
				ToolInput: json.RawMessage(`{"queries":[{"keywords":"payment error rate","query":"payment error rate high investigation runbook"}]}`),
			},
		}},
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{
				Type:       "tool_result",
				ToolUseID:  "tu_1",
				ToolName:   "search_skills",
				ToolResult: []investigations.ToolResultPart{{Type: "text", Text: xmlResult}},
			},
		}},
	}

	matches := investigations.ExtractSkillMatches(messages)
	require.Len(t, matches, 2)

	assert.Equal(t, "78ffce9d-8911-4f8b-8a16-6b42f646ca2d", matches[0].SkillID)
	assert.Equal(t, "Investigate payment error spikes", matches[0].Title)
	assert.Equal(t, "payment error rate high investigation runbook", matches[0].Query)
	assert.Contains(t, matches[0].Chunk, "Confirm symptom")
	assert.Contains(t, matches[1].Chunk, `"deploy generation"`) // HTML entity decoded
}

func TestExtractSkillMatches_MultipleQueries(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{
				Type:      "tool_use",
				ToolID:    "tu_1",
				ToolName:  "search_skills",
				ToolInput: json.RawMessage(`{"queries":[{"query":"first"},{"query":"second"}]}`),
			},
		}},
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{
				Type:       "tool_result",
				ToolUseID:  "tu_1",
				ToolResult: []investigations.ToolResultPart{{Type: "text", Text: `<results count="1"><chunk skill_id="s1" title="t">body</chunk></results>`}},
			},
		}},
	}
	matches := investigations.ExtractSkillMatches(messages)
	require.Len(t, matches, 1)
	assert.Equal(t, "first; second", matches[0].Query)
}

func TestExtractSkillMatches_IgnoresOtherTools(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{Type: "tool_use", ToolID: "tu_1", ToolName: "prometheus_query_handler"},
		}},
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{Type: "tool_result", ToolUseID: "tu_1", ToolResult: []investigations.ToolResultPart{{Type: "text", Text: `<results count="1"><chunk skill_id="s1" title="t">x</chunk></results>`}}},
		}},
	}
	assert.Empty(t, investigations.ExtractSkillMatches(messages))
}
