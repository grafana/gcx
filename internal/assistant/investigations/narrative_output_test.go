package investigations //nolint:testpackage // exercises the unexported narrativeOpts wiring

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Agent-output-contract tests for `investigations narrative`: the command no
// longer overrides the agents codec with raw markdown, so the agent-mode
// default emits exactly one JSON value (the narrative as a JSON string) like
// every sibling investigations command, while the human default keeps the
// raw markdown byte-identically.

const narrativeFixture = "## Findings\n\np99 latency spiked at 14:02."

func setAgentModeInv(t *testing.T, enabled bool) {
	t.Helper()
	agent.SetFlag(enabled)
	t.Cleanup(agent.ResetForTesting)
}

func TestNarrativeAgentModeEmitsSingleJSONValue(t *testing.T) {
	setAgentModeInv(t, true)
	opts := &narrativeOpts{}
	opts.setup(pflag.NewFlagSet("narrative", pflag.ContinueOnError))
	require.Equal(t, "agents", opts.IO.OutputFormat, "agent mode must fall through to the standard agents codec")

	var out, errOut bytes.Buffer
	opts.IO.ErrWriter = &errOut
	require.NoError(t, opts.IO.Encode(&out, narrativeFixture))

	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var got string
	require.NoError(t, dec.Decode(&got), "agent-mode narrative output must be one JSON value, got: %s", out.String())
	assert.Equal(t, narrativeFixture, got, "the prose payload is carried verbatim inside the JSON string")
	assert.False(t, dec.More(), "exactly one JSON value on stdout")
}

func TestNarrativeHumanDefaultUnchanged(t *testing.T) {
	setAgentModeInv(t, false)
	opts := &narrativeOpts{}
	opts.setup(pflag.NewFlagSet("narrative", pflag.ContinueOnError))
	require.Equal(t, "table", opts.IO.OutputFormat)

	var out bytes.Buffer
	require.NoError(t, opts.IO.Encode(&out, narrativeFixture))
	assert.Equal(t, narrativeFixture+"\n", out.String(), "human default stays raw markdown with a trailing newline")
}

func TestNarrativeExplicitJSONUnchanged(t *testing.T) {
	setAgentModeInv(t, false)
	opts := &narrativeOpts{}
	opts.setup(pflag.NewFlagSet("narrative", pflag.ContinueOnError))
	opts.IO.OutputFormat = "json"

	var out bytes.Buffer
	require.NoError(t, opts.IO.Encode(&out, narrativeFixture))
	var got string
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "-o json keeps the JSON-quoted string: %s", out.String())
	assert.Equal(t, narrativeFixture, got)
}
