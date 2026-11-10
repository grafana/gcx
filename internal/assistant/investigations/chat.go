package investigations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
)

// Chat endpoints live on the v1 surface; not affected by the v2 investigations
// rollout (grafana-assistant-app#6645).
const chatAllMessagesFmt = "/api/v1/chats/%s/all-messages"

// ChatThreadMessage is one message in a Lodestone chat thread. The thread
// interleaves user prompts, assistant prose, tool invocations, tool results,
// and panel artifacts — all the substantive content of an investigation
// lives here.
type ChatThreadMessage struct {
	ID        string             `json:"id"`
	Role      string             `json:"role"`
	Type      string             `json:"type,omitempty"`
	Hidden    bool               `json:"hidden,omitempty"`
	CreatedAt string             `json:"created,omitempty"`
	Audience  string             `json:"audience,omitempty"`
	Content   []ChatContentBlock `json:"content"`
}

// ChatContentBlock uses the shared legacy chat wire model.
type ChatContentBlock = assistant.ContentBlock

// GetChatThread fetches the full message list for a Lodestone chat. Messages
// are returned in server order (chronological by `sequence`).
func (c *Client) GetChatThread(ctx context.Context, chatID string) ([]ChatThreadMessage, error) {
	path := fmt.Sprintf(chatAllMessagesFmt, url.PathEscape(chatID))
	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get chat thread: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}
	var envelope struct {
		Data struct {
			Messages []ChatThreadMessage `json:"messages"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode chat thread: %w", err)
	}
	if envelope.Data.Messages == nil {
		return []ChatThreadMessage{}, nil
	}
	return envelope.Data.Messages, nil
}

// Narrative returns the assistant-authored prose from a chat thread —
// text-type content blocks only, with <context>...</context> tags stripped.
// Thinking blocks and tool plumbing are excluded; this is the workspace
// reader's view.
func Narrative(messages []ChatThreadMessage) string {
	var sb strings.Builder
	for _, m := range messages {
		if m.Role != "assistant" || m.Hidden {
			continue
		}
		for _, b := range m.Content {
			if b.Type != "text" || b.Text == "" {
				continue
			}
			text := assistant.StripContextTags(b.Text)
			if text == "" {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(text)
		}
	}
	return sb.String()
}

// ToolCall is a single tool_use block paired with its matching tool_result.
// Used by the `list-tool-calls` subcommand. Result keeps the original part list so
// `-o json` is lossless; the table codec joins parts for display.
type ToolCall struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
	DurationMs json.Number     `json:"durationMs,omitempty"`
}

// ExtractToolCalls walks the chat thread and pairs every tool_use with its
// tool_result (via toolUseId == toolId). Tool uses with no matching result
// (e.g. still running) come back with an empty Result.
func ExtractToolCalls(messages []ChatThreadMessage) []ToolCall {
	results := map[string]ChatContentBlock{}
	for _, m := range messages {
		for _, b := range m.Content {
			if b.Type == "tool_result" && b.ToolUseID != "" {
				results[b.ToolUseID] = b
			}
		}
	}

	var calls []ToolCall
	for _, m := range messages {
		for _, b := range m.Content {
			if b.Type != "tool_use" {
				continue
			}
			call := ToolCall{
				ID:    b.ToolID,
				Name:  b.ToolName,
				Input: b.ToolInput,
			}
			if r, ok := results[b.ToolID]; ok {
				call.Result = r.ToolResult
				call.IsError = r.IsError
				call.DurationMs = r.DurationMs
			}
			calls = append(calls, call)
		}
	}
	return calls
}
