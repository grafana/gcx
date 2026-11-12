package investigations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/assistant"
)

// errorBlockType is the content block a failed aisdk turn (a `data-error`
// part) converts to. Legacy chats have no equivalent.
const errorBlockType = "error"

// uiPart holds the aisdk message part fields the converter reads. Tool parts
// are typed "tool-<name>", or "dynamic-tool" for MCP tools; the server also
// sends toolName.
type uiPart struct {
	Type       string          `json:"type"`
	Text       string          `json:"text"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	State      string          `json:"state"`
	Input      json.RawMessage `json:"input"`
	Output     json.RawMessage `json:"output"`
	ErrorText  string          `json:"errorText"`
	Data       json.RawMessage `json:"data"`
}

type uiToolOutput struct {
	Content    json.RawMessage `json:"content"`
	IsError    bool            `json:"isError"`
	DurationMs *float64        `json:"durationMs"`
}

// threadFromUIMessages converts aisdk UI messages to the legacy thread shape,
// so list-messages, get-narrative and list-tool-calls behave the same on both
// engines. One UI message can become several thread messages, in part order:
//
//	assistant UI message            legacy-shaped thread
//	  text                    →     assistant  [text, tool_use]
//	  tool-x (output)               user       [tool_result]      (as legacy)
//	  reasoning               →     assistant  [thinking]  hidden
//	  text                    →     assistant  [text]
//
// The first piece keeps the message ID; later pieces get "<id>:<n>". Reasoning
// is hidden (shown with --include-hidden). step-start parts carry no content
// and are dropped. Parts gcx has no mapping for become a block that keeps the
// part's type and its JSON in Raw: JSON and YAML output show it, text output
// and the narrative skip it.
func threadFromUIMessages(messages []assistant.ChatMessage) ([]ChatThreadMessage, error) {
	out := make([]ChatThreadMessage, 0, len(messages))
	for _, m := range messages {
		converted, err := threadFromUIMessage(m)
		if err != nil {
			return nil, err
		}
		out = append(out, converted...)
	}
	return out, nil
}

func threadFromUIMessage(m assistant.ChatMessage) ([]ChatThreadMessage, error) {
	var parts []json.RawMessage
	if err := json.Unmarshal(m.Parts, &parts); err != nil {
		return nil, fmt.Errorf("decode Assistant UI message %q parts: %w", m.ID, err)
	}

	var out []ChatThreadMessage
	piece := func(role string, hidden bool, content []ChatContentBlock) {
		id := m.ID
		if len(out) > 0 {
			id = m.ID + ":" + strconv.Itoa(len(out))
		}
		out = append(out, ChatThreadMessage{
			ID:        id,
			Role:      role,
			Type:      "message",
			Hidden:    m.Hidden || hidden,
			CreatedAt: m.CreatedAt,
			Content:   content,
		})
	}
	var current []ChatContentBlock
	flush := func() {
		if len(current) > 0 {
			piece(m.Role, false, current)
			current = nil
		}
	}

	for _, raw := range parts {
		var p uiPart
		if err := json.Unmarshal(raw, &p); err != nil || p.Type == "" {
			return nil, fmt.Errorf("invalid part in Assistant UI message %q", m.ID)
		}
		switch {
		case p.Type == "text":
			current = append(current, ChatContentBlock{Type: "text", Text: p.Text})
		case p.Type == "reasoning":
			flush()
			piece(m.Role, true, []ChatContentBlock{{Type: "thinking", Thinking: p.Text}})
		case p.Type == "step-start":
		case p.Type == "data-error":
			current = append(current, ChatContentBlock{Type: errorBlockType, Text: dataErrorText(p.Data)})
		case strings.HasPrefix(p.Type, "tool-") || p.Type == "dynamic-tool":
			name := p.ToolName
			if name == "" {
				name = strings.TrimPrefix(p.Type, "tool-")
			}
			current = append(current, ChatContentBlock{
				Type:      "tool_use",
				ToolID:    p.ToolCallID,
				ToolName:  name,
				ToolInput: nonNull(p.Input),
			})
			if p.State != "output-available" && p.State != "output-error" && p.State != "output-denied" {
				continue
			}
			flush()
			piece("user", false, []ChatContentBlock{toolResultBlock(p, name)})
		default:
			current = append(current, ChatContentBlock{Type: p.Type, Raw: append(json.RawMessage(nil), raw...)})
		}
	}
	flush()
	return out, nil
}

// toolResultBlock builds the legacy tool_result for a finished tool part.
// output.content is the part list legacy results carry; other outputs (for
// example a launched extension) are kept as they are. A failed call with no
// content reports its errorText as the result text.
func toolResultBlock(p uiPart, name string) ChatContentBlock {
	block := ChatContentBlock{
		Type:      "tool_result",
		ToolUseID: p.ToolCallID,
		ToolName:  name,
		IsError:   p.State == "output-error" || p.State == "output-denied",
	}
	if output := nonNull(p.Output); output != nil {
		var decoded uiToolOutput
		if json.Unmarshal(output, &decoded) == nil {
			block.IsError = block.IsError || decoded.IsError
			block.DurationMs = durationMs(decoded.DurationMs)
		}
		block.ToolResult = output
		if content, ok := contentField(output); ok {
			block.ToolResult = nonNull(content)
		}
	}
	if block.ToolResult == nil && p.ErrorText != "" {
		block.ToolResult = textResult(p.ErrorText)
	}
	return block
}

func durationMs(v *float64) json.Number {
	if v == nil {
		return ""
	}
	return json.Number(strconv.FormatFloat(*v, 'f', -1, 64))
}

// textResult builds a legacy tool result part list holding one text part.
func textResult(text string) json.RawMessage {
	encoded, err := json.Marshal([]struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{{Type: "text", Text: text}})
	if err != nil {
		return nil
	}
	return encoded
}

// contentField returns a tool output object's content key, if it has one.
func contentField(raw json.RawMessage) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, false
	}
	v, ok := fields["content"]
	return v, ok
}

func dataErrorText(raw json.RawMessage) string {
	var data struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &data) == nil {
		return data.Text
	}
	return ""
}

// errorText is the message shown for an error block with no text.
func errorText(text string) string {
	if strings.TrimSpace(text) == "" {
		return "no error details"
	}
	return strings.Join(strings.Fields(text), " ")
}

func nonNull(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	return raw
}
