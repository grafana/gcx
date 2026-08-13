package evaluators //nolint:testpackage // Tests drive the unexported runDelete seam and deleteOpts wiring.

// Contract tests for `gcx agento11y evaluators delete` (#387 agent output
// contract). The shared engine's edge cases live in commandutil; these tests
// pin this command's wiring: the silent human default, the per-id receipt
// strings, the gcx.mutation_batch document in agent mode, the partial-failure
// EmittedError, and the explicit -o override.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDeleteOptsForTest(t *testing.T, output string) *deleteOpts {
	t.Helper()
	opts := &deleteOpts{}
	opts.IO.ErrWriter = io.Discard // silence the agent-mode --json/--jq hint
	flags := pflag.NewFlagSet("delete", pflag.ContinueOnError)
	opts.setup(flags)
	if output != "" {
		require.NoError(t, flags.Set("output", output))
	}
	require.NoError(t, opts.IO.Validate())
	return opts
}

func TestDelete_OutputContract(t *testing.T) {
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })

	del := func(fail string) func(string) error {
		return func(id string) error {
			if id == fail {
				return errors.New("boom")
			}
			return nil
		}
	}

	tests := []struct {
		name       string
		agentMode  bool
		output     string
		ids        []string
		failID     string
		wantStdout string // exact match; "" means empty
		wantJSON   bool   // stdout must be exactly one JSON value
		wantStderr string // exact match when non-empty and not agent mode
		wantErr    bool
	}{
		{
			name:       "human default stays byte-identical (empty stdout, stderr receipts)",
			ids:        []string{"e-1", "e-2"},
			wantStdout: "",
			wantStderr: "✔ Deleted evaluator e-1\n✔ Deleted evaluator e-2\n",
		},
		{
			name:      "agent mode emits exactly one JSON value",
			agentMode: true,
			ids:       []string{"e-1"},
			wantJSON:  true,
		},
		{
			name:     "explicit -o json override honored",
			output:   "json",
			ids:      []string{"e-1"},
			wantJSON: true,
		},
		{
			name:      "partial failure returns EmittedError with exit 4",
			agentMode: true,
			ids:       []string{"e-1", "e-2"},
			failID:    "e-2",
			wantJSON:  true,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(agent.ResetForTesting)

			opts := newDeleteOptsForTest(t, tc.output)
			var stdout, stderr bytes.Buffer
			err := runDelete(&stdout, &stderr, opts, tc.ids, del(tc.failID))

			if tc.wantErr {
				var emitted *gcxerrors.EmittedError
				require.ErrorAs(t, err, &emitted)
				assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
			} else {
				require.NoError(t, err)
			}

			if tc.wantJSON {
				dec := json.NewDecoder(strings.NewReader(stdout.String()))
				var doc map[string]any
				require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", stdout.String())
				var second any
				require.ErrorIs(t, dec.Decode(&second), io.EOF,
					"stdout must contain exactly one JSON value:\n%s", stdout.String())
				assert.Equal(t, "gcx.mutation_batch", doc["type"])
				assert.Equal(t, "deleted", doc["action"])
				return
			}

			assert.Equal(t, tc.wantStdout, stdout.String())
			if tc.wantStderr != "" {
				assert.Equal(t, tc.wantStderr, stderr.String())
			}
		})
	}
}

func TestDelete_ExplicitYAMLOverride(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	opts := newDeleteOptsForTest(t, "yaml")
	var stdout, stderr bytes.Buffer
	require.NoError(t, runDelete(&stdout, &stderr, opts, []string{"e-1"}, func(string) error { return nil }))
	assert.Contains(t, stdout.String(), "type: gcx.mutation_batch")
}
