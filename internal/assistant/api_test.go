package assistant_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/assistant"
)

func TestFetchChatMessages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/api/plugins/grafana-assistant-app/resources/api/v1/chats/chat-1/all-messages" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"messages": []assistant.ChatMessage{
					{
						ID:   "m1",
						Role: "user",
						Content: assistant.ContentJSON{
							{Type: "text", Text: "Why is checkout slow?"},
						},
					},
					{
						ID:   "m2",
						Role: "assistant",
						Content: assistant.ContentJSON{
							{Type: "text", Text: "p99 latency is elevated."},
						},
					},
				},
			},
		})
	}))
	t.Cleanup(server.Close)

	messages, err := assistant.FetchChatMessages(
		context.Background(),
		server.URL+"/api/plugins/grafana-assistant-app/resources/api/v1",
		"test-token",
		"chat-1",
		server.Client(),
	)
	if err != nil {
		t.Fatalf("FetchChatMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(messages))
	}
	if messages[0].ExtractText() != "Why is checkout slow?" {
		t.Fatalf("message 0 text = %q", messages[0].ExtractText())
	}
}

func TestFetchChats(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/api/plugins/grafana-assistant-app/resources/api/v1/chats" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("source"); got != "assistant,cli" {
			t.Errorf("source query = %q, want %q", got, "assistant,cli")
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("limit query = %q, want %q", got, "10")
		}
		if got := r.URL.Query().Get("includeArchived"); got != "true" {
			t.Errorf("includeArchived query = %q, want %q", got, "true")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"items": []assistant.Chat{
					{ID: "chat-1", Name: "Checkout latency", Source: "assistant", UpdatedAt: "2026-05-30T12:00:00Z"},
					{ID: "chat-2", Name: "Disk pressure", Source: "cli"},
					{ID: "chat-3", Name: "Inline edit", Source: "inline-assistant"},
				},
			},
		})
	}))
	t.Cleanup(server.Close)

	chats, err := assistant.FetchChats(
		context.Background(),
		server.URL+"/api/plugins/grafana-assistant-app/resources/api/v1",
		"test-token",
		assistant.ListChatsOptions{Source: "assistant,cli", Limit: 10, IncludeArchived: true},
		server.Client(),
	)
	if err != nil {
		t.Fatalf("FetchChats: %v", err)
	}
	// chat-3 is an inline-assistant chat and must be filtered out.
	if len(chats) != 2 {
		t.Fatalf("got %d chats, want 2", len(chats))
	}
	if chats[0].ID != "chat-1" || chats[0].Name != "Checkout latency" {
		t.Fatalf("chat 0 = %+v", chats[0])
	}
	for _, c := range chats {
		if c.Source == "inline-assistant" {
			t.Fatalf("inline-assistant chat %s was not filtered out", c.ID)
		}
	}
}

func TestConversationTranscript_FormatText(t *testing.T) {
	t.Parallel()

	transcript := assistant.ConversationTranscript{
		Chat: assistant.Chat{
			ID:     "chat-1",
			Name:   "Checkout latency",
			Source: "assistant",
		},
		Messages: []assistant.ChatMessage{
			{
				Role: "user",
				Content: assistant.ContentJSON{
					{Type: "text", Text: "Why is checkout slow?"},
				},
			},
			{
				Role:    "internal",
				Content: assistant.ContentJSON{{Type: "text", Text: "hidden system"}},
			},
			{
				Role: "assistant",
				Content: assistant.ContentJSON{
					{Type: "text", Text: "p99 latency is elevated."},
				},
			},
		},
	}

	out := transcript.FormatText()
	for _, want := range []string{
		"Conversation: Checkout latency",
		"ID: chat-1",
		"Source: assistant",
		"USER:",
		"Why is checkout slow?",
		"ASSISTANT:",
		"p99 latency is elevated.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatText() missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "hidden system") {
		t.Fatalf("FormatText() should omit internal messages\n%s", out)
	}
}
