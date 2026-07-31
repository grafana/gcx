package services //nolint:testpackage // Tests drive the unexported notFoundEmitted helper and per-command opts wiring.

// Contract tests for the encode-then-not-found paths of the four services
// query commands (get, map, list-operations, list-labels) under the #387
// agent output contract. These commands deliberately emit the (empty)
// snapshot document and then signal the no-data outcome via the exit code.
// Previously the not-found return was a bare error, so agent mode got a
// SECOND error JSON document appended to stdout by reportError. Now the
// paths return notFoundEmitted: a typed stderr diagnostic plus an
// EmittedError carrying the same exit code 1, with stdout left holding
// exactly one JSON value.

import (
	"bytes"
	"encoding/json"
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

func TestNotFoundEmitted(t *testing.T) {
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })

	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human mode prose diagnostic", agentMode: false},
		{name: "agent mode JSONL diagnostic", agentMode: true},
	}

	const msg = `service "payments/checkout" has no telemetry in the requested window`

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(agent.ResetForTesting)

			var stderr bytes.Buffer
			err := notFoundEmitted(&stderr, msg)

			// The error carries the pre-migration exit code 1 without a
			// second stdout document.
			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted, "notFoundEmitted must return *gcxerrors.EmittedError, got %T", err)
			assert.Equal(t, gcxerrors.ExitGeneralError, emitted.Code)
			assert.Contains(t, err.Error(), msg)

			if tc.agentMode {
				var record map[string]any
				require.NoError(t, json.Unmarshal(stderr.Bytes(), &record),
					"agent-mode stderr diagnostic must be JSONL: %q", stderr.String())
				assert.Equal(t, "warning", record["class"])
				assert.Equal(t, msg, record["summary"])
				return
			}

			assert.Equal(t, "warn: "+msg+"\n", stderr.String())
		})
	}
}

// TestNotFound_AgentStdoutSingleDocument replays each command's
// encode-then-not-found sequence in agent mode and asserts stdout holds
// exactly one JSON value followed by EOF — the snapshot document — with the
// failure signaled out-of-band.
func TestNotFound_AgentStdoutSingleDocument(t *testing.T) {
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })

	tests := []struct {
		name string
		// encode performs the command's Encode call with opts built exactly
		// the way the command's setup builds them.
		encode func(t *testing.T, stdout io.Writer) error
	}{
		{
			name: "services get",
			encode: func(t *testing.T, stdout io.Writer) error {
				t.Helper()
				opts := &getOpts{}
				opts.IO.ErrWriter = io.Discard
				opts.setup(pflag.NewFlagSet("get", pflag.ContinueOnError))
				return opts.IO.Encode(stdout, &ServiceDetail{})
			},
		},
		{
			name: "services get --group-by",
			encode: func(t *testing.T, stdout io.Writer) error {
				t.Helper()
				opts := &getOpts{}
				opts.IO.ErrWriter = io.Discard
				opts.setup(pflag.NewFlagSet("get", pflag.ContinueOnError))
				return opts.IO.Encode(stdout, &GroupedServiceDetail{GroupBy: []string{"k8s_cluster_name"}})
			},
		},
		{
			name: "services map",
			encode: func(t *testing.T, stdout io.Writer) error {
				t.Helper()
				opts := &mapOpts{}
				opts.IO.ErrWriter = io.Discard
				opts.setup(pflag.NewFlagSet("map", pflag.ContinueOnError))
				return opts.IO.Encode(stdout, &ServiceMap{})
			},
		},
		{
			name: "services list-operations",
			encode: func(t *testing.T, stdout io.Writer) error {
				t.Helper()
				opts := &operationsOpts{}
				opts.IO.ErrWriter = io.Discard
				opts.setup(pflag.NewFlagSet("list-operations", pflag.ContinueOnError))
				return opts.IO.Encode(stdout, &OperationsResponse{})
			},
		},
		{
			name: "services list-labels",
			encode: func(t *testing.T, stdout io.Writer) error {
				t.Helper()
				opts := &labelsOpts{}
				opts.IO.ErrWriter = io.Discard
				opts.setup(pflag.NewFlagSet("list-labels", pflag.ContinueOnError))
				return opts.IO.Encode(stdout, &ServiceLabelsResponse{})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(true)
			t.Cleanup(agent.ResetForTesting)

			var stdout, stderr bytes.Buffer
			require.NoError(t, tc.encode(t, &stdout))
			err := notFoundEmitted(&stderr, "no telemetry")

			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted)
			assert.Equal(t, gcxerrors.ExitGeneralError, emitted.Code)

			// stdout must hold exactly one JSON value.
			dec := json.NewDecoder(strings.NewReader(stdout.String()))
			var first any
			require.NoError(t, dec.Decode(&first), "stdout is not valid JSON:\n%s", stdout.String())
			var second any
			require.ErrorIs(t, dec.Decode(&second), io.EOF,
				"stdout must contain exactly one JSON value:\n%s", stdout.String())
		})
	}
}

// TestNotFound_HumanTableDefaultUnchanged pins the human default: the table
// codec output on stdout is unchanged and the not-found diagnostic lands on
// stderr, so scripts scraping stdout see exactly what they saw before.
func TestNotFound_HumanTableDefaultUnchanged(t *testing.T) {
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })

	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	opts := &getOpts{}
	opts.IO.ErrWriter = io.Discard
	opts.setup(pflag.NewFlagSet("get", pflag.ContinueOnError))
	require.Equal(t, "table", opts.IO.OutputFormat, "human default must remain the table codec")

	var stdout, stderr bytes.Buffer
	detail := &ServiceDetail{Service: Service{Name: "checkout", Namespace: "payments"}}
	require.NoError(t, opts.IO.Encode(&stdout, detail))
	err := notFoundEmitted(&stderr, `service "payments/checkout" has no telemetry in the requested window`)

	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	assert.Contains(t, stdout.String(), "Name:")
	assert.Contains(t, stdout.String(), "checkout")
	assert.NotContains(t, stdout.String(), "warn:", "the diagnostic must not leak into stdout")
	assert.Equal(t, "warn: service \"payments/checkout\" has no telemetry in the requested window\n", stderr.String())
}
