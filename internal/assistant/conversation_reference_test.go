package assistant_test

import (
	"testing"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConversationReference(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantID     string
		wantShared bool
		wantErr    string
	}{
		{name: "opaque ID", input: "chat-1", wantID: "chat-1"},
		{name: "shared URL", input: "https://example.grafana.net/a/grafana-assistant-app/chats/shared/shared-1?from=nav#message", wantID: "shared-1", wantShared: true},
		{name: "shared URL under base path", input: "https://example.grafana.net/grafana/a/grafana-assistant-app/chats/shared/shared-1", wantID: "shared-1", wantShared: true},
		{name: "empty", input: "", wantErr: "conversation reference must not be empty"},
		{name: "slash in ID", input: "chat/1", wantErr: "single path segment"},
		{name: "backslash in ID", input: `chat\\1`, wantErr: "single path segment"},
		{name: "traversal", input: "..", wantErr: "invalid conversation ID"},
		{name: "control", input: "chat\n1", wantErr: "control"},
		{name: "userinfo", input: "https://user@example.grafana.net/a/grafana-assistant-app/chats/shared/shared-1", wantErr: "credentials"},
		{name: "unsupported URL", input: "https://example.grafana.net/a/grafana-assistant-app/chats/chat-1", wantErr: "unsupported conversation URL"},
		{name: "additional segment", input: "https://example.grafana.net/a/grafana-assistant-app/chats/shared/shared-1/extra", wantErr: "unsupported conversation URL"},
		{name: "encoded separator", input: "https://example.grafana.net/a/grafana-assistant-app/chats/shared/shared%2F1", wantErr: "single path segment"},
		{name: "malformed URL", input: "https://[bad", wantErr: "invalid conversation URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := assistant.ParseConversationReference(tt.input)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, got.ID)
			assert.Equal(t, tt.wantShared, got.Shared)
		})
	}
}

func TestConversationReferenceValidateGrafanaURL(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		grafanaURL string
		wantErr    string
	}{
		{name: "plain ID needs no origin validation", input: "chat-1", grafanaURL: "https://other.example.net"},
		{name: "same origin", input: "https://EXAMPLE.grafana.net/a/grafana-assistant-app/chats/shared/shared-1", grafanaURL: "https://example.grafana.net"},
		{name: "scheme comparison is case insensitive", input: "HTTPS://example.grafana.net/a/grafana-assistant-app/chats/shared/shared-1", grafanaURL: "https://example.grafana.net"},
		{name: "default HTTPS port", input: "https://example.grafana.net:443/a/grafana-assistant-app/chats/shared/shared-1", grafanaURL: "https://example.grafana.net"},
		{name: "default HTTP port", input: "http://example.grafana.net:80/a/grafana-assistant-app/chats/shared/shared-1", grafanaURL: "http://example.grafana.net"},
		{name: "base path", input: "https://example.grafana.net/grafana/a/grafana-assistant-app/chats/shared/shared-1", grafanaURL: "https://example.grafana.net/grafana/"},
		{name: "origin mismatch", input: "https://other.example.net/a/grafana-assistant-app/chats/shared/shared-1", grafanaURL: "https://example.grafana.net", wantErr: "does not match the selected Grafana context"},
		{name: "scheme mismatch", input: "http://example.grafana.net/a/grafana-assistant-app/chats/shared/shared-1", grafanaURL: "https://example.grafana.net", wantErr: "does not match the selected Grafana context"},
		{name: "base path mismatch", input: "https://example.grafana.net/other/a/grafana-assistant-app/chats/shared/shared-1", grafanaURL: "https://example.grafana.net/grafana", wantErr: "does not match the selected Grafana context"},
		{name: "path traversal is not normalized", input: "https://example.grafana.net/other/../a/grafana-assistant-app/chats/shared/shared-1", grafanaURL: "https://example.grafana.net", wantErr: "does not match the selected Grafana context"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := assistant.ParseConversationReference(tt.input)
			require.NoError(t, err)
			err = ref.ValidateGrafanaURL(tt.grafanaURL)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
