package assistant_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	assistantcmd "github.com/grafana/gcx/cmd/gcx/assistant"
	"github.com/grafana/gcx/internal/assistant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationGetCommand_TextOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugins/grafana-assistant-app/resources/api/v1/chats/chat-1":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": assistant.Chat{
					ID:     "chat-1",
					Name:   "Checkout latency",
					Source: "assistant",
				},
			})
		case "/api/plugins/grafana-assistant-app/resources/api/v1/chats/chat-1/all-messages":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"messages": []assistant.ChatMessage{
						{
							Role: "user",
							Content: assistant.ContentJSON{
								{Type: "text", Text: "Why is checkout slow?"},
							},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfgPath := writeAssistantTestConfig(t, server.URL)

	root := assistantcmd.Command()
	root.SetContext(context.Background())
	root.SilenceUsage = true
	root.SilenceErrors = true

	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"conversation", "get", "chat-1", "--config", cfgPath, "--output", "text"})

	require.NoError(t, root.Execute())

	out := stdout.String()
	require.Contains(t, out, "Conversation: Checkout latency")
	require.Contains(t, out, "Why is checkout slow?")
}

func TestConversationListCommand_TableOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/plugins/grafana-assistant-app/resources/api/v1/chats" {
			http.NotFound(w, r)
			return
		}
		assert.Equal(t, "all", r.URL.Query().Get("source"))
		assert.Equal(t, "25", r.URL.Query().Get("limit"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"items": []assistant.Chat{
					{
						ID:        "chat-1",
						Name:      "Checkout latency",
						Source:    "assistant",
						UpdatedAt: "2026-05-30T12:00:00Z",
					},
					{
						ID:     "chat-2",
						Name:   "Disk pressure",
						Source: "cli",
					},
				},
			},
		})
	}))
	t.Cleanup(server.Close)

	cfgPath := writeAssistantTestConfig(t, server.URL)

	root := assistantcmd.Command()
	root.SetContext(context.Background())
	root.SilenceUsage = true
	root.SilenceErrors = true

	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"conversation", "list", "--config", cfgPath, "--source", "all", "--limit", "25", "--output", "table"})

	require.NoError(t, root.Execute())

	out := stdout.String()
	require.Contains(t, out, "ID")
	require.Contains(t, out, "TITLE")
	require.Contains(t, out, "chat-1")
	require.Contains(t, out, "Checkout latency")
	require.Contains(t, out, "2026-05-30 12:00")
	require.Contains(t, out, "chat-2")
	require.Contains(t, out, "Disk pressure")
}

func TestConversationCommand_Validation(t *testing.T) {
	cfgPath := writeAssistantTestConfig(t, "http://127.0.0.1:0")

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "list negative limit",
			args:    []string{"conversation", "list", "--config", cfgPath, "--limit", "-1"},
			wantErr: "--limit must not be negative",
		},
		{
			name:    "list negative offset",
			args:    []string{"conversation", "list", "--config", cfgPath, "--offset", "-1"},
			wantErr: "--offset must not be negative",
		},
		{
			name:    "list conflicting archive flags",
			args:    []string{"conversation", "list", "--config", cfgPath, "--include-archived", "--archived-only"},
			wantErr: "cannot use both --include-archived and --archived-only flags",
		},
		{
			name:    "list non-positive timeout",
			args:    []string{"conversation", "list", "--config", cfgPath, "--timeout", "0"},
			wantErr: "--timeout must be positive",
		},
		{
			name:    "get non-positive timeout",
			args:    []string{"conversation", "get", "chat-1", "--config", cfgPath, "--timeout", "0"},
			wantErr: "--timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := assistantcmd.Command()
			root.SetContext(context.Background())
			root.SilenceUsage = true
			root.SilenceErrors = true
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(tt.args)

			err := root.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestConversationCommand_Registered(t *testing.T) {
	root := assistantcmd.Command()

	for _, sub := range []string{"get", "list"} {
		cmd, _, err := root.Find([]string{"conversation", sub})
		require.NoError(t, err)
		require.Equal(t, sub, cmd.Name())
	}
}

func writeAssistantTestConfig(t *testing.T, grafanaURL string) string {
	t.Helper()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `current-context: test
contexts:
  test:
    grafana:
      server: ` + grafanaURL + `
      stack-id: 12345
      token: test-token
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0o600))
	return cfgPath
}
