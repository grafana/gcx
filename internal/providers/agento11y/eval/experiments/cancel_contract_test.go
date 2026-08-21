package experiments //nolint:testpackage // Tests drive the unexported emitCancelReceipt seam and cancelOpts wiring.

// Contract tests for `gcx agento11y experiments cancel` (#387 agent output
// contract): the silent human default (receipt on stderr, exactly as
// before), the gcx.mutation document in agent mode, and explicit -o
// overrides. Cancel is a single atomic call, so there is no partial-failure
// path: an API failure returns before anything reaches stdout.

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCancelOptsForTest(t *testing.T, output string) *cancelOpts {
	t.Helper()
	opts := &cancelOpts{}
	opts.IO.ErrWriter = io.Discard // silence the agent-mode --json/--jq hint
	flags := pflag.NewFlagSet("cancel", pflag.ContinueOnError)
	opts.setup(flags)
	if output != "" {
		require.NoError(t, flags.Set("output", output))
	}
	require.NoError(t, opts.IO.Validate())
	return opts
}

func TestCancel_OutputContract(t *testing.T) {
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })

	tests := []struct {
		name       string
		agentMode  bool
		output     string
		wantStderr string
		wantDoc    map[string]any
	}{
		{
			name:       "human default stays byte-identical (empty stdout, stderr receipt)",
			wantStderr: "✔ Experiment r-1 canceled\n",
		},
		{
			name:      "agent mode emits exactly one JSON value",
			agentMode: true,
			wantDoc: map[string]any{
				"type":           "gcx.mutation",
				"schema_version": "1",
				"action":         "canceled",
			},
		},
		{
			name:    "explicit -o json override honored",
			output:  "json",
			wantDoc: map[string]any{"type": "gcx.mutation", "action": "canceled"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(agent.ResetForTesting)

			opts := newCancelOptsForTest(t, tc.output)
			var stdout, stderr bytes.Buffer
			require.NoError(t, emitCancelReceipt(&stdout, &stderr, opts, "r-1"))

			if tc.wantDoc != nil {
				dec := json.NewDecoder(strings.NewReader(stdout.String()))
				var doc map[string]any
				require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", stdout.String())
				var second any
				require.ErrorIs(t, dec.Decode(&second), io.EOF,
					"stdout must contain exactly one JSON value:\n%s", stdout.String())
				for key, want := range tc.wantDoc {
					assert.Equal(t, want, doc[key], "field %q", key)
				}
				target, ok := doc["target"].(map[string]any)
				require.True(t, ok, "target must be an object: %v", doc["target"])
				assert.Equal(t, "experiment", target["kind"])
				assert.Equal(t, "r-1", target["id"])
				return
			}

			assert.Empty(t, stdout.String(), "default human stdout must stay empty")
			assert.Equal(t, tc.wantStderr, stderr.String())
		})
	}
}

func TestCancel_ExplicitYAMLOverride(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	opts := newCancelOptsForTest(t, "yaml")
	var stdout, stderr bytes.Buffer
	require.NoError(t, emitCancelReceipt(&stdout, &stderr, opts, "r-1"))
	assert.Contains(t, stdout.String(), "type: gcx.mutation")
	assert.Contains(t, stdout.String(), "action: canceled")
}
