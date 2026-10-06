//go:build !windows

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Exercise the real main and HTTP exporter. An identity is useful only if
// both fields reach the receiver without the session or override value.
func TestAgentIdentityReachesUsageReceiver(t *testing.T) {
	type identityCase struct {
		name string
		env  []string
		want string
		mode bool
	}
	tests := make([]identityCase, 0, 28)
	tests = append(tests, []identityCase{
		{"codex shell", []string{"CODEX_SHELL=1"}, "codex", true},
		{"codex thread", []string{"CODEX_THREAD_ID=synthetic-private-session"}, "codex", true},
		{"gemini shell", []string{"GEMINI_CLI=1"}, "gemini-cli", true},
		{"copilot CLI", []string{"COPILOT_CLI=1"}, "github-copilot", true},
		{"kilo fork", []string{"KILO=1", "OPENCODE=1"}, "kilo-code", true},
		{"shared name", []string{"AI_AGENT=goose@synthetic-private-session"}, "goose", true},
		{"mode opt out", []string{"CODEX_SHELL=1", "GCX_AGENT_MODE=false"}, "codex", false},
		{"unknown name", []string{"GCX_AGENT_NAME=synthetic-private-session"}, "", false},
	}...)
	for _, name := range []string{"claude-code", "codex", "cursor", "github-copilot", "gemini-cli", "opencode", "cline", "kilo-code", "kiro", "factory-droid", "amp", "augment", "junie", "devin", "openhands", "goose", "aider", "qwen-code", "pi", "crush"} {
		tests = append(tests, identityCase{name, []string{"GCX_AGENT_NAME=" + name}, name, true})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan []byte, 1)
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				events <- body
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(receiver.Close)
			keys := agent.EnvironmentVariables()
			env := make([]string, 0, len(keys)+len(tc.env))
			for _, key := range keys {
				env = append(env, key+"=")
			}
			env = append(env, tc.env...)
			// Help needs no Grafana request, but still exports one usage event.
			helper := startUsageEventHelperEnv(t, "http://127.0.0.1", receiver.URL, env, "commands", "--help")
			require.NoError(t, helper.cmd.Wait(), "stderr=%s", helper.stderr.String())
			body := recvWithin(t, events, "the agent usage event")
			fields := decodeEvent(t, body)
			assert.Equal(t, tc.want, fields["agent"])
			assert.Equal(t, tc.mode, fields["is_agent"])
			assert.NotContains(t, string(body), "synthetic-private-session")
		})
	}
}
