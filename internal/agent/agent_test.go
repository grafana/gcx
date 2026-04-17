package agent_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/stretchr/testify/assert"
)

func TestIsAgentMode(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		setFlag  *bool // nil = don't call SetFlag
		wantMode bool
	}{
		{
			name:     "no env vars set",
			wantMode: false,
		},
		{
			name:     "CLAUDE_CODE=1",
			envVars:  map[string]string{"CLAUDE_CODE": "1"},
			wantMode: true,
		},
		{
			name:     "CURSOR_AGENT=true",
			envVars:  map[string]string{"CURSOR_AGENT": "true"},
			wantMode: true,
		},
		{
			name:     "GITHUB_COPILOT=yes",
			envVars:  map[string]string{"GITHUB_COPILOT": "yes"},
			wantMode: true,
		},
		{
			name:     "AMAZON_Q=1",
			envVars:  map[string]string{"AMAZON_Q": "1"},
			wantMode: true,
		},
		{
			name:     "OPENCODE=1",
			envVars:  map[string]string{"OPENCODE": "1"},
			wantMode: true,
		},
		{
			name:     "GCX_AGENT_MODE=1",
			envVars:  map[string]string{"GCX_AGENT_MODE": "1"},
			wantMode: true,
		},
		{
			name: "GCX_AGENT_MODE=0 overrides CLAUDE_CODE=1",
			envVars: map[string]string{
				"GCX_AGENT_MODE": "0",
				"CLAUDE_CODE":    "1",
			},
			wantMode: false,
		},
		{
			name: "GCX_AGENT_MODE=false overrides CURSOR_AGENT=1",
			envVars: map[string]string{
				"GCX_AGENT_MODE": "false",
				"CURSOR_AGENT":   "1",
			},
			wantMode: false,
		},
		{
			name:     "GCX_AGENT_MODE=no disables agent mode",
			envVars:  map[string]string{"GCX_AGENT_MODE": "no"},
			wantMode: false,
		},
		{
			name:     "SetFlag(true) with no env vars enables agent mode",
			setFlag:  new(bool),
			wantMode: false, // new(bool) is false; override below
		},
		{
			name:     "SetFlag(false) overrides env detection (explicit --agent=false)",
			envVars:  map[string]string{"CLAUDE_CODE": "1"},
			setFlag:  new(false),
			wantMode: false,
		},
		{
			name:     "case insensitive truthy: CLAUDE_CODE=TRUE",
			envVars:  map[string]string{"CLAUDE_CODE": "TRUE"},
			wantMode: true,
		},
		{
			name:     "case insensitive truthy: CLAUDE_CODE=Yes",
			envVars:  map[string]string{"CLAUDE_CODE": "Yes"},
			wantMode: true,
		},
		{
			name:     "case insensitive falsy: GCX_AGENT_MODE=FALSE",
			envVars:  map[string]string{"GCX_AGENT_MODE": "FALSE"},
			wantMode: false,
		},
		{
			name:     "case insensitive falsy: GCX_AGENT_MODE=No",
			envVars:  map[string]string{"GCX_AGENT_MODE": "No"},
			wantMode: false,
		},
	}

	// Fix the SetFlag(true) test case
	tests[9].setFlag = new(true)
	tests[9].wantMode = true

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Clear all agent env vars so host env (e.g. CLAUDECODE=1 in
			// Claude Code) does not leak into test cases.
			clearAgentEnv(t)

			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			agent.ResetForTesting()

			if tc.setFlag != nil {
				agent.SetFlag(*tc.setFlag)
			}

			assert.Equal(t, tc.wantMode, agent.IsAgentMode())
		})
	}
}

// clearAgentEnv unsets all agent-mode env vars for the duration of a test.
func clearAgentEnv(t *testing.T) {
	t.Helper()

	for _, env := range []string{
		"GCX_AGENT_MODE",
		"CLAUDECODE",
		"CLAUDE_CODE",
		"CURSOR_AGENT",
		"GITHUB_COPILOT",
		"AMAZON_Q",
		"OPENCODE",
	} {
		t.Setenv(env, "")
	}
}

func TestDetectedFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		setFlag *bool
		wantEnv bool
	}{
		{
			name:    "returns true when env var set",
			envVars: map[string]string{"CLAUDE_CODE": "1"},
			wantEnv: true,
		},
		{
			name:    "returns false when only SetFlag used",
			setFlag: new(true),
			wantEnv: false,
		},
		{
			name:    "returns false when no detection at all",
			wantEnv: false,
		},
		{
			name:    "returns false when GCX_AGENT_MODE=0",
			envVars: map[string]string{"GCX_AGENT_MODE": "0"},
			wantEnv: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearAgentEnv(t)

			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			agent.ResetForTesting()

			if tc.setFlag != nil {
				agent.SetFlag(*tc.setFlag)
			}

			assert.Equal(t, tc.wantEnv, agent.DetectedFromEnv())
		})
	}
}
