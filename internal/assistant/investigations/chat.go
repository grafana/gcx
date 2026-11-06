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
// rollout (grafana-assistant-app#6645). Each chat's engine is fixed at creation
// and selects its transcript endpoint: legacy chats serve all-messages, aisdk
// chats serve ui-messages. The server rejects the other one with HTTP 400
// "chat engine mismatch".
const (
	chatFmt            = "/api/v1/chats/%s"
	chatAllMessagesFmt = chatFmt + "/all-messages"
	chatUIMessagesFmt  = chatFmt + "/ui-messages?thread=main"
)

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

// GetChatThread fetches the full message list for an investigation chat, in
// server order. It reads the chat's engine first and uses that engine's
// transcript endpoint. aisdk messages are converted to the legacy thread shape
// (see threadFromUIMessages), so callers and output formats see one shape.
func (c *Client) GetChatThread(ctx context.Context, chatID string) ([]ChatThreadMessage, error) {
	engine, err := c.chatEngine(ctx, chatID)
	if err != nil {
		return nil, err
	}
	switch engine {
	case "", "legacy":
		return c.legacyChatThread(ctx, chatID)
	case "aisdk":
		return c.uiChatThread(ctx, chatID)
	default:
		return nil, fmt.Errorf("chat %s uses engine %q, which this gcx version cannot read", chatID, engine)
	}
}

func (c *Client) chatEngine(ctx context.Context, chatID string) (string, error) {
	var envelope struct {
		Data assistant.Chat `json:"data"`
	}
	if err := c.getChatJSON(ctx, fmt.Sprintf(chatFmt, url.PathEscape(chatID)), "get chat", &envelope); err != nil {
		return "", err
	}
	return envelope.Data.Engine, nil
}

func (c *Client) legacyChatThread(ctx context.Context, chatID string) ([]ChatThreadMessage, error) {
	var envelope struct {
		Data struct {
			Messages []ChatThreadMessage `json:"messages"`
		} `json:"data"`
	}
	if err := c.getChatJSON(ctx, fmt.Sprintf(chatAllMessagesFmt, url.PathEscape(chatID)), "get chat thread", &envelope); err != nil {
		return nil, err
	}
	if envelope.Data.Messages == nil {
		return []ChatThreadMessage{}, nil
	}
	return envelope.Data.Messages, nil
}

func (c *Client) uiChatThread(ctx context.Context, chatID string) ([]ChatThreadMessage, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := c.getChatJSON(ctx, fmt.Sprintf(chatUIMessagesFmt, url.PathEscape(chatID)), "get chat thread", &envelope); err != nil {
		return nil, err
	}
	messages, err := assistant.DecodeUIMessages(envelope.Data)
	if err != nil {
		return nil, err
	}
	return threadFromUIMessages(messages)
}

func (c *Client) getChatJSON(ctx context.Context, path, operation string, target any) error {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return assistanthttp.HandleErrorResponse(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode %s response: %w", operation, err)
	}
	return nil
}

// Narrative returns the assistant-authored prose from a chat thread —
// text-type content blocks only, with <context>...</context> tags stripped.
// Thinking blocks and tool plumbing are excluded; this is the workspace
// reader's view. A failed aisdk turn adds a one-line "[turn failed: ...]"
// note so the reader knows the prose stops early.
func Narrative(messages []ChatThreadMessage) string {
	var sb strings.Builder
	for _, m := range messages {
		if m.Role != "assistant" || m.Hidden {
			continue
		}
		for _, b := range m.Content {
			var text string
			switch {
			case b.Type == "text" && b.Text != "":
				text = assistant.StripContextTags(b.Text)
			case b.Type == errorBlockType && b.Raw == nil && !b.Received():
				text = "[turn failed: " + errorText(b.Text) + "]"
			default:
				continue
			}
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
