package assistant_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/assistant"
)

func TestValidateResumableChatSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		wantNotice bool
		wantErr    bool
	}{
		{name: "cli", source: "cli"},
		{name: "assistant", source: "assistant", wantNotice: true},
		{name: "a2a", source: "a2a", wantNotice: true},
		{name: "slack", source: "slack", wantNotice: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			notice, err := assistant.ValidateResumableChatSource("chat-id", &assistant.Chat{
				ID:     "chat-id",
				Source: tt.source,
			})

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNotice && notice == "" {
				t.Fatal("expected notice for non-cli source")
			}
			if !tt.wantNotice && notice != "" {
				t.Fatalf("expected empty notice, got %q", notice)
			}
		})
	}
}

func TestValidateCLIContext_ResumesWebChat(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/plugins/grafana-assistant-app/resources/api/v1/chats/web-chat-id" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": assistant.Chat{
				ID:     "web-chat-id",
				Source: "assistant",
			},
		})
	}))
	t.Cleanup(server.Close)

	client := assistant.New(assistant.ClientOptions{
		GrafanaURL: server.URL,
		Token:      "test-token",
	})

	notice, err := client.ValidateCLIContext(context.Background(), "web-chat-id")
	if err != nil {
		t.Fatalf("ValidateCLIContext() error = %v", err)
	}
	if notice == "" {
		t.Fatal("expected notice for web conversation")
	}
}

func TestValidateCLIContext_RejectsMissingChat(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	client := assistant.New(assistant.ClientOptions{
		GrafanaURL: server.URL,
		Token:      "test-token",
	})

	_, err := client.ValidateCLIContext(context.Background(), "missing-chat-id")
	if err == nil {
		t.Fatal("expected error when chat does not exist")
	}
}
