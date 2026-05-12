package investigations_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
						"content": []map[string]any{
							{"type": "text", "text": "Looking at the API latency."},
							{"type": "tool_use", "id": "tu_1", "name": "search_skills", "input": map[string]any{"query": "api latency"}},
						},
					},
					{
						"id":   "m2",
						"role": "tool",
						"content": []map[string]any{
							{
								"type":        "tool_result",
								"tool_use_id": "tu_1",
								"durationMs":  42,
								"content":     json.RawMessage(`{"results":[{"id":"s1","name":"APILatency","score":0.91,"chunk":"check p99"}]}`),
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
	assert.Equal(t, "search_skills", messages[0].Content[1].Name)
	assert.Equal(t, "tool_result", messages[1].Content[0].Type)
	assert.Equal(t, "tu_1", messages[1].Content[0].ToolUseID)
	assert.Equal(t, int64(42), messages[1].Content[0].DurationMs)
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
			{Type: "tool_use", Name: "prometheus_query_handler"},
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
			{Type: "tool_use", ID: "tu_1", Name: "search_skills", Input: json.RawMessage(`{"query":"q"}`)},
			{Type: "tool_use", ID: "tu_2", Name: "prometheus_query_handler"},
		}},
		{Role: "tool", Content: []investigations.ChatContentBlock{
			{Type: "tool_result", ToolUseID: "tu_1", DurationMs: 12, ToolResult: json.RawMessage(`{"ok":true}`)},
			{Type: "tool_result", ToolUseID: "tu_2", IsError: true, ToolResult: json.RawMessage(`{"error":"x"}`)},
		}},
	}
	calls := investigations.ExtractToolCalls(messages)
	require.Len(t, calls, 2)
	assert.Equal(t, "search_skills", calls[0].Name)
	assert.Equal(t, int64(12), calls[0].DurationMs)
	assert.False(t, calls[0].IsError)
	assert.True(t, calls[1].IsError)
}

func TestExtractToolCalls_PendingResult(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{Type: "tool_use", ID: "tu_1", Name: "loki_query_handler_investigator"},
		}},
	}
	calls := investigations.ExtractToolCalls(messages)
	require.Len(t, calls, 1)
	assert.Empty(t, calls[0].Result)
	assert.Zero(t, calls[0].DurationMs)
}

func TestExtractSkillMatches_FlatObject(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{Type: "tool_use", ID: "tu_1", Name: "search_skills", Input: json.RawMessage(`{"query":"api latency"}`)},
		}},
		{Role: "tool", Content: []investigations.ChatContentBlock{
			{
				Type:       "tool_result",
				ToolUseID:  "tu_1",
				ToolResult: json.RawMessage(`{"results":[{"id":"s1","name":"APILatency","score":0.9,"chunk":"check p99"},{"id":"s2","name":"DBSlow","score":0.7,"chunk":"see DB stats"}]}`),
			},
		}},
	}
	matches := investigations.ExtractSkillMatches(messages)
	require.Len(t, matches, 2)
	assert.Equal(t, "APILatency", matches[0].SkillName)
	assert.Equal(t, "api latency", matches[0].Query)
	assert.InDelta(t, 0.9, matches[0].Score, 0.001)
	assert.Equal(t, "check p99", matches[0].Chunk)
}

func TestExtractSkillMatches_AnthropicNested(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{Type: "tool_use", ID: "tu_1", Name: "search_skills", Input: json.RawMessage(`{"query":"latency"}`)},
		}},
		{Role: "tool", Content: []investigations.ChatContentBlock{
			{
				Type:       "tool_result",
				ToolUseID:  "tu_1",
				ToolResult: json.RawMessage(`[{"type":"text","text":"{\"results\":[{\"id\":\"s1\",\"name\":\"APILatency\",\"score\":0.85,\"chunk\":\"hot path\"}]}"}]`),
			},
		}},
	}
	matches := investigations.ExtractSkillMatches(messages)
	require.Len(t, matches, 1)
	assert.Equal(t, "APILatency", matches[0].SkillName)
	assert.Equal(t, "hot path", matches[0].Chunk)
}

func TestExtractSkillMatches_IgnoresOtherTools(t *testing.T) {
	messages := []investigations.ChatThreadMessage{
		{Role: "assistant", Content: []investigations.ChatContentBlock{
			{Type: "tool_use", ID: "tu_1", Name: "prometheus_query_handler"},
		}},
		{Role: "tool", Content: []investigations.ChatContentBlock{
			{Type: "tool_result", ToolUseID: "tu_1", ToolResult: json.RawMessage(`{"results":[{"name":"x"}]}`)},
		}},
	}
	assert.Empty(t, investigations.ExtractSkillMatches(messages))
}
