package investigations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
)

// Chat endpoints live on the v1 surface; not affected by the v2 investigations
// rollout (grafana-assistant-app#6645).
const chatAllMessagesFmt = "/api/v1/chats/%s/all-messages"

// ChatThreadMessage is one message in a Lodestone chat thread. The thread
// interleaves user prompts, assistant prose, tool invocations, tool results,
// and panel artifacts — all the substantive content lives here, not in the
// legacy timeline/todos/report endpoints (which return empty stubs for
// Lodestone).
type ChatThreadMessage struct {
	ID        string             `json:"id"`
	Role      string             `json:"role"`
	Type      string             `json:"type,omitempty"`
	Hidden    bool               `json:"hidden,omitempty"`
	CreatedAt string             `json:"created,omitempty"`
	Audience  string             `json:"audience,omitempty"`
	Content   []ChatContentBlock `json:"content"`
}

// ChatContentBlock is one entry in a message's content array. Field tags
// follow the actual Grafana Assistant plugin shape (toolName/toolInput/
// toolUseId/toolResult — camelCase), not the Anthropic content-block
// convention. Tool calls and tool results are first-class because they're
// how Lodestone exposes what the agent did.
type ChatContentBlock struct {
	Type         string           `json:"type"`
	Text         string           `json:"text,omitempty"`
	Thinking     string           `json:"thinking,omitempty"`
	ToolID       string           `json:"toolId,omitempty"`
	ToolName     string           `json:"toolName,omitempty"`
	ToolInput    json.RawMessage  `json:"toolInput,omitempty"`
	ToolUseID    string           `json:"toolUseId,omitempty"`
	ToolResult   []ToolResultPart `json:"toolResult,omitempty"`
	IsError      bool             `json:"isError,omitempty"`
	DurationMs   int64            `json:"durationMs,omitempty"`
	ArtifactType string           `json:"artifactType,omitempty"`
	Panel        json.RawMessage  `json:"panel,omitempty"`
}

// ToolResultPart is one entry in the tool_result envelope. The server may
// emit multiple parts (e.g. one text block per query in a multi-query call).
type ToolResultPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

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
			text := stripContextTags(b.Text)
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
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Input      json.RawMessage  `json:"input,omitempty"`
	Result     []ToolResultPart `json:"result,omitempty"`
	IsError    bool             `json:"isError,omitempty"`
	DurationMs int64            `json:"durationMs,omitempty"`
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

// stripContextTags removes <context>...</context> blocks injected by the
// server. Duplicates the helper in internal/assistant/types.go (unexported
// there) to keep this package free of cross-package dependencies.
func stripContextTags(text string) string {
	for {
		start := strings.Index(text, "<context>")
		if start == -1 {
			break
		}
		end := strings.Index(text, "</context>")
		if end == -1 {
			break
		}
		text = strings.TrimSpace(text[:start] + text[end+len("</context>"):])
	}
	return text
}
