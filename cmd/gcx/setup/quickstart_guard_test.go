//nolint:testpackage // white-box testing: drives the unexported prompting guard, runner, and run directly.
package setup

import (
	"context"
	"io"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuickstartPromptingAllowed(t *testing.T) {
	tests := []struct {
		name       string
		stdinIsTTY bool
		agentMode  bool
		want       bool
	}{
		{name: "TTY human", stdinIsTTY: true, agentMode: false, want: true},
		{name: "TTY agent (PTY-driven) must not prompt", stdinIsTTY: true, agentMode: true, want: false},
		{name: "no TTY human", stdinIsTTY: false, agentMode: false, want: false},
		{name: "no TTY agent", stdinIsTTY: false, agentMode: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent.SetFlag(tt.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			assert.Equal(t, tt.want, promptingAllowed(tt.stdinIsTTY))
		})
	}
}

func TestRunQuickstart_NonInteractive_WithoutUseDefaults_FailsFast(t *testing.T) {
	promptCalls := 0
	r := &quickstartRunner{
		contacts: &fakeContactPoints{},
		rules:    &fakeAlertRules{},
		stdout:   io.Discard,
		stderr:   io.Discard,
		isTTY:    false,
		promptYesNo: func(string, bool) (bool, error) {
			promptCalls++
			return false, nil
		},
		promptText: func(string, string) (string, error) {
			promptCalls++
			return "", nil
		},
	}

	err := runQuickstart(context.Background(), &quickstartOpts{}, r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--use-defaults")
	assert.Zero(t, promptCalls, "no wizard prompt may fire on the non-interactive path")
}

func TestRunQuickstart_AgentMode_NeverPrompts(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	assert.False(t, promptingAllowed(true), "agent mode must disable prompts even on a TTY")
}
