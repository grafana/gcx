package assistant

import (
	"fmt"
	"strings"
)

// ConversationTranscript is a chat and its message history.
type ConversationTranscript struct {
	Chat     Chat          `json:"chat"`
	Messages []ChatMessage `json:"messages"`
}

// VisibleMessages returns user/assistant messages with extractable text, in API order.
func (t ConversationTranscript) VisibleMessages() []ChatMessage {
	if len(t.Messages) == 0 {
		return nil
	}
	visible := make([]ChatMessage, 0, len(t.Messages))
	for _, m := range t.Messages {
		if m.Hidden {
			continue
		}
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(m.ExtractText()) == "" {
			continue
		}
		visible = append(visible, m)
	}
	return visible
}

// FormatText renders a human-readable transcript for coding agents and terminals.
func (t ConversationTranscript) FormatText() string {
	var b strings.Builder

	title := strings.TrimSpace(t.Chat.Name)
	if title == "" {
		title = "(untitled)"
	}
	fmt.Fprintf(&b, "Conversation: %s\n", title)
	fmt.Fprintf(&b, "ID: %s\n", t.Chat.ID)
	if t.Chat.Source != "" {
		fmt.Fprintf(&b, "Source: %s\n", t.Chat.Source)
	}

	visible := t.VisibleMessages()
	if len(visible) == 0 {
		b.WriteString("\n(no user/assistant messages)\n")
		return b.String()
	}

	for _, m := range visible {
		role := strings.ToUpper(m.Role)
		b.WriteByte('\n')
		fmt.Fprintf(&b, "%s:\n", role)
		b.WriteString(m.ExtractText())
		b.WriteByte('\n')
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}
