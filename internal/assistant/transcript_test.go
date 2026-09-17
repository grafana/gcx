package assistant_test

import (
	"testing"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/stretchr/testify/assert"
)

func TestConversationTranscriptFormatTextShowsSharedAISDKScope(t *testing.T) {
	transcript := assistant.ConversationTranscript{
		Chat: assistant.Chat{ID: "shared-1", Name: "Synthetic", Source: "assistant", Engine: "aisdk", Shared: true},
		Messages: []assistant.ChatMessage{{
			Role:    "assistant",
			Content: assistant.ContentJSON{{Type: "text", Text: "hello"}},
		}},
		Scope: "main",
	}

	got := transcript.FormatText()
	assert.Contains(t, got, "Shared snapshot: yes")
	assert.Contains(t, got, "Scope: main thread")
	assert.Contains(t, got, "hello")
}

func TestConversationTranscriptFormatTextKeepsLegacyMetadataMinimal(t *testing.T) {
	transcript := assistant.ConversationTranscript{
		Chat: assistant.Chat{ID: "chat-1", Name: "Legacy", Source: "assistant", Engine: "legacy"},
		Messages: []assistant.ChatMessage{{
			Role:    "user",
			Content: assistant.ContentJSON{{Type: "text", Text: "question"}},
		}},
	}

	got := transcript.FormatText()
	assert.NotContains(t, got, "Shared snapshot")
	assert.NotContains(t, got, "Scope:")
}

func TestConversationTranscriptFormatTextExplainsNonProseParts(t *testing.T) {
	transcript := assistant.ConversationTranscript{
		Chat: assistant.Chat{ID: "chat-1", Name: "Tools", Engine: "aisdk"},
		Messages: []assistant.ChatMessage{{
			ID:    "m1",
			Role:  "assistant",
			Parts: []byte(`[{"type":"tool-example","toolCallId":"call-1"}]`),
		}},
		Scope: "main",
	}

	assert.Contains(t, transcript.FormatText(), "no displayable user/assistant prose; use --output json to inspect message parts")
}

func TestConversationTranscriptFormatTextStillReportsTrulyEmptyConversation(t *testing.T) {
	transcript := assistant.ConversationTranscript{Chat: assistant.Chat{ID: "chat-1", Name: "Empty"}, Messages: []assistant.ChatMessage{}}

	assert.Contains(t, transcript.FormatText(), "(no user/assistant messages)")
}
