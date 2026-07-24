//nolint:testpackage // white-box testing: drives the unexported prompting guard, runner, and run directly.
package setup

// Pins the agent-aware interactivity guard: setup's wizard prompts must never
// fire in agent mode, even when gcx runs on a PTY (an agent would hang on the
// y/n questions). Agent mode forces the non-interactive path, which fails
// fast with the --use-defaults guidance.

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	instrumentation "github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptingAllowed(t *testing.T) {
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

// TestRun_NonInteractive_WithoutUseDefaults_FailsFast pins the fail-fast
// behavior the guard routes agents into: no prompt function is ever invoked
// and the error carries the --use-defaults guidance.
func TestRun_NonInteractive_WithoutUseDefaults_FailsFast(t *testing.T) {
	promptCalls := 0
	r := &runner{
		client: &fakeSetupClient{},
		stdout: io.Discard,
		stderr: io.Discard,
		isTTY:  false,
		promptFn: func(string, bool) (bool, error) {
			promptCalls++
			return false, nil
		},
	}

	err := run(context.Background(), &opts{}, "prod-eu", r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--use-defaults")
	assert.Zero(t, promptCalls, "no wizard prompt may fire on the non-interactive path")
}

// fakeSetupClient is a minimal clientInterface stub for the guard test: the
// discovery and read steps succeed so run reaches resolveDesired.
type fakeSetupClient struct{}

func (fakeSetupClient) SetupK8sDiscovery(context.Context, instrumentation.BackendURLs, instrumentation.PromHeaders) error {
	return nil
}

func (fakeSetupClient) GetK8SInstrumentation(context.Context, string) (*instrumentation.GetK8SInstrumentationResponse, error) {
	return &instrumentation.GetK8SInstrumentationResponse{}, nil
}

func (fakeSetupClient) SetK8SInstrumentation(context.Context, string, instrumentation.Cluster, instrumentation.BackendURLs) error {
	return errors.New("must not be called")
}
