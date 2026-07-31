package overrides //nolint:testpackage // Tests drive the unexported writeUpdateReceipt seam and updateOpts wiring.

// Contract tests for `gcx appo11y overrides update` (#387 agent output
// contract): the human default stdout stays byte-identical to the
// pre-migration cmdio.Success one-liner, agent mode emits exactly one
// gcx.mutation JSON document, and explicit -o overrides are honored. Update
// is a single atomic call, so there is no partial-failure path: an API
// failure returns before anything reaches stdout.

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

func newUpdateOptsForTest(t *testing.T, output string) *updateOpts {
	t.Helper()
	opts := &updateOpts{}
	opts.IO.ErrWriter = io.Discard // silence the agent-mode --json/--jq hint
	flags := pflag.NewFlagSet("update", pflag.ContinueOnError)
	opts.setup(flags)
	if output != "" {
		require.NoError(t, flags.Set("output", output))
	}
	opts.File = "overrides.yaml" // satisfy Validate; the receipt path never reads it
	require.NoError(t, opts.Validate())
	return opts
}

func TestUpdateReceipt_OutputContract(t *testing.T) {
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })

	tests := []struct {
		name       string
		agentMode  bool
		output     string
		wantStdout string // exact match when wantDoc is nil
		wantDoc    map[string]any
	}{
		{
			name:       "human default stays byte-identical",
			wantStdout: "✔ Overrides updated successfully.\n",
		},
		{
			name:      "agent mode emits exactly one JSON value",
			agentMode: true,
			wantDoc: map[string]any{
				"type":           "gcx.mutation",
				"schema_version": "1",
				"action":         "updated",
			},
		},
		{
			name:    "explicit -o json override honored",
			output:  "json",
			wantDoc: map[string]any{"type": "gcx.mutation", "action": "updated"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(agent.ResetForTesting)

			opts := newUpdateOptsForTest(t, tc.output)
			var stdout bytes.Buffer
			require.NoError(t, writeUpdateReceipt(&stdout, opts))

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
				assert.Equal(t, "Overrides", target["kind"])
				assert.Equal(t, "default", target["name"])
				return
			}

			assert.Equal(t, tc.wantStdout, stdout.String(), "default human stdout must stay byte-identical")
		})
	}
}

func TestUpdateReceipt_ExplicitYAMLOverride(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	opts := newUpdateOptsForTest(t, "yaml")
	var stdout bytes.Buffer
	require.NoError(t, writeUpdateReceipt(&stdout, opts))
	assert.Contains(t, stdout.String(), "type: gcx.mutation")
	assert.Contains(t, stdout.String(), "action: updated")
	assert.NotContains(t, stdout.String(), "✔", "explicit -o yaml must not carry the styled human line")
}
