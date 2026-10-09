package assistant_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/stretchr/testify/assert"
)

func TestConversationTranscriptFormatTextShowsSharedAISDKScope(t *testing.T) {
	transcript := assistant.ConversationTranscript{
		Chat: assistant.Chat{ID: "shared-1", Name: "Synthetic", Source: "assistant", Engine: "aisdk", Shared: true},
		Messages: []assistant.ChatMessage{
			{Role: "assistant", Content: assistant.ContentJSON{{Type: "text", Text: "hello"}}},
			{Role: "user", Content: assistant.ContentJSON{{Type: "text", Text: "follow-up"}}},
			{Role: "assistant", Hidden: true, Content: assistant.ContentJSON{{Type: "text", Text: "hidden detail"}}},
		},
		Scope: "main",
	}

	got := transcript.FormatText()
	assert.Contains(t, got, "Shared snapshot: yes")
	assert.Contains(t, got, "Scope: main thread")
	assert.Contains(t, got, "hello")
	assert.Contains(t, got, "follow-up")
	assert.Less(t, strings.Index(got, "hello"), strings.Index(got, "follow-up"))
	assert.NotContains(t, got, "hidden detail")
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

func TestConversationTranscriptFormatTextOmissionHint(t *testing.T) {
	tests := []struct {
		name                             string
		prose, hidden, nontext, wantHint bool
	}{
		{"mixed prose and parts", true, false, true, true},
		{"parts only", false, false, true, true},
		{"hidden parts with visible prose", true, true, true, false},
		{"hidden parts only", false, true, true, false},
		{"text only", true, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transcript := assistant.ConversationTranscript{Chat: assistant.Chat{ID: "chat-1"}}
			if tt.prose {
				transcript.Messages = append(transcript.Messages, assistant.ChatMessage{Role: "assistant", Content: assistant.ContentJSON{{Type: "text", Text: "visible prose"}}})
			}
			if tt.nontext {
				transcript.Messages = append(transcript.Messages, assistant.ChatMessage{Role: "assistant", Hidden: tt.hidden, Parts: []byte(`[{"type":"file","url":"private"}]`)})
			}
			got := transcript.FormatText()
			assert.Equal(t, tt.wantHint, strings.Contains(got, "use --output json to inspect message parts"))
			if tt.prose {
				assert.Contains(t, got, "visible prose")
			}
			assert.NotContains(t, got, "private")
		})
	}
}
