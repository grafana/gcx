package agent_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/stretchr/testify/assert"
)

func TestHarnessSignals(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"codex shell", map[string]string{"CODEX_SHELL": "1"}, "codex"},
		{"codex thread", map[string]string{"CODEX_THREAD_ID": "synthetic-thread-id"}, "codex"},
		{"codex session", map[string]string{"CODEX_SESSION_ID": "synthetic-session-id"}, "codex"},
		{"gemini shell", map[string]string{"GEMINI_CLI": "1"}, "gemini-cli"},
		{"copilot CLI", map[string]string{"COPILOT_CLI": "1"}, "github-copilot"},
		{"copilot session", map[string]string{"COPILOT_AGENT_SESSION_ID": "synthetic-session-id"}, "github-copilot"},
		{"cline terminal", map[string]string{"CLINE_ACTIVE": "true"}, "cline"},
		{"goose terminal", map[string]string{"GOOSE_TERMINAL": "1"}, "goose"},
		{"kilo before opencode", map[string]string{"KILO": "1", "OPENCODE": "1", "AGENT": "1"}, "kilo-code"},
		{"qwen before gemini", map[string]string{"QWEN_CODE": "1", "GEMINI_CLI": "1"}, "qwen-code"},
		{"explicit nested agent", map[string]string{"GCX_AGENT_NAME": "crush", "CLAUDECODE": "1"}, "crush"},
		{"shared identity before native", map[string]string{"AI_AGENT": "junie", "CLAUDECODE": "1"}, "junie"},
		{"explicit before shared", map[string]string{"GCX_AGENT_NAME": "devin", "AI_AGENT": "amp"}, "devin"},
		{"shared version discarded", map[string]string{"AI_AGENT": "github-copilot-cli@synthetic-private-version"}, "github-copilot"},
		{"normalized explicit name", map[string]string{"GCX_AGENT_NAME": "  CoDeX  "}, "codex"},
		{"kilo alias", map[string]string{"AI_AGENT": "kilo"}, "kilo-code"},
		{"kilocode alias", map[string]string{"AGENT": "kilocode"}, "kilo-code"},
		{"droid alias", map[string]string{"GCX_AGENT_NAME": "droid"}, "factory-droid"},
		{"auggie alias", map[string]string{"AGENT": "auggie"}, "augment"},
		{"goose shared identity", map[string]string{"AGENT": "goose"}, "goose"},
		{"unknown identity falls through", map[string]string{"GCX_AGENT_NAME": "private-project", "CODEX_SHELL": "1"}, "codex"},
		{"unknown identity ignored", map[string]string{"GCX_AGENT_NAME": "private-project"}, ""},
		{"unknown shared identity ignored", map[string]string{"AI_AGENT": "private-project", "AGENT": "1"}, ""},
		{"config and credentials are not identity", map[string]string{"AMP_HOME": "/tmp/amp", "AUGMENT_API_TOKEN": "synthetic-token", "TERM_PROGRAM": "kiro", "OPENAI_API_KEY": "synthetic-token"}, ""},
		{"false session ignored", map[string]string{"CODEX_THREAD_ID": "false"}, ""},
		{"boolean marker needs boolean", map[string]string{"CODEX_SHELL": "arbitrary", "GEMINI_CLI": "0"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearAgentEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			agent.ResetForTesting()
			assert.Equal(t, tc.want, agent.Name())
			assert.Equal(t, tc.want != "", agent.IsAgentMode())
			assert.Equal(t, tc.want != "", agent.DetectedFromEnv())
		})
	}
}

func TestSupportedHarnessNames(t *testing.T) {
	names := []string{"claude-code", "codex", "cursor", "github-copilot", "gemini-cli", "opencode", "cline", "kilo-code", "kiro", "factory-droid", "amp", "augment", "junie", "devin", "openhands", "goose", "aider", "qwen-code", "pi", "crush", "amazon-q"}
	for _, name := range names {
		for _, env := range []string{"GCX_AGENT_NAME", "AI_AGENT", "AGENT"} {
			t.Run(name+"/"+env, func(t *testing.T) {
				clearAgentEnv(t)
				t.Setenv(env, name)
				agent.ResetForTesting()
				assert.Equal(t, name, agent.Name())
				assert.True(t, agent.IsAgentMode())
				assert.True(t, agent.DetectedFromEnv())
			})
		}
	}
}

func TestHarnessModeOverrides(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		flag     *bool
		mode     bool
		identity string
	}{
		{"mode off retains identity", map[string]string{"GCX_AGENT_MODE": "false", "GCX_AGENT_NAME": "junie"}, nil, false, "junie"},
		{"flag off retains codex", map[string]string{"CODEX_SHELL": "1"}, new(false), false, "codex"},
		{"flag wins over mode off", map[string]string{"GCX_AGENT_MODE": "false", "GEMINI_CLI": "1"}, new(true), true, "gemini-cli"},
		{"mode on needs no name", map[string]string{"GCX_AGENT_MODE": "true"}, nil, true, ""},
		{"invalid override does not disable detection", map[string]string{"GCX_AGENT_MODE": "invalid", "CODEX_THREAD_ID": "synthetic-id"}, nil, true, "codex"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearAgentEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			agent.ResetForTesting()
			if tc.flag != nil {
				agent.SetFlag(*tc.flag)
			}
			assert.Equal(t, tc.mode, agent.IsAgentMode())
			assert.Equal(t, tc.identity, agent.Name())
		})
	}
}
