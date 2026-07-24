package commandutil_test

// These tests pin the pre-GA agent output contract for the shared batch
// delete engine used by the Agent Observability delete verbs (evaluators,
// rules, guards, saved-conversations, collections):
//
//   - default human stdout stays byte-identical to the pre-migration
//     behavior: EMPTY (per-id receipts live on stderr);
//   - agent mode emits EXACTLY ONE JSON value on stdout, then EOF
//     (gcx.mutation_batch);
//   - a mid-loop failure after at least one success is a partial failure:
//     *gcxerrors.EmittedError with ExitPartialFailure, the complete document
//     already on stdout, and unattempted ids counted as skipped;
//   - a first-id failure returns the raw wrapped error (nothing was written
//     to stdout, so the standard fused-error path stays honest);
//   - explicit -o json / -o yaml overrides are honored.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/aio11y/commandutil"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withPlainColors(t *testing.T) {
	t.Helper()
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })
}

func setAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	agent.SetFlag(enabled)
	t.Cleanup(agent.ResetForTesting)
}

// newIOForTest builds cmdio.Options exactly the way the delete commands do:
// silent text codec, text default, flags bound (which resolves the agent-mode
// default). agent.SetFlag must be called before this.
func newIOForTest(t *testing.T, output string) *cmdio.Options {
	t.Helper()
	opts := &cmdio.Options{}
	opts.ErrWriter = io.Discard // silence the agent-mode --json/--jq hint
	flags := pflag.NewFlagSet("delete", pflag.ContinueOnError)
	opts.RegisterCustomCodec("text", commandutil.SilentTextCodec{})
	opts.DefaultFormat("text")
	opts.BindFlags(flags)
	if output != "" {
		require.NoError(t, flags.Set("output", output))
	}
	require.NoError(t, opts.Validate())
	return opts
}

// failOn returns a delete func failing for the ids in bad.
func failOn(bad ...string) func(string) error {
	return func(id string) error {
		if slices.Contains(bad, id) {
			return errors.New("boom")
		}
		return nil
	}
}

// decodeSingleJSONValue asserts that raw holds exactly one JSON value
// followed by EOF, and returns it.
func decodeSingleJSONValue(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", raw)
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF, "stdout must contain exactly one JSON value:\n%s", raw)
	return doc
}

func runBatchDelete(t *testing.T, opts *cmdio.Options, ids []string, del func(string) error) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := commandutil.RunBatchDelete(&stdout, &stderr, opts,
		"evaluator", "Deleted evaluator %s", "deleting evaluator %s", ids, del)
	return stdout.String(), stderr.String(), err
}

func TestRunBatchDelete_HumanDefault_ByteIdentical(t *testing.T) {
	withPlainColors(t)
	setAgentMode(t, false)

	tests := []struct {
		name       string
		ids        []string
		del        func(string) error
		wantStderr string
	}{
		{
			name:       "single id",
			ids:        []string{"e-1"},
			del:        failOn(),
			wantStderr: "✔ Deleted evaluator e-1\n",
		},
		{
			name:       "multiple ids",
			ids:        []string{"e-1", "e-2"},
			del:        failOn(),
			wantStderr: "✔ Deleted evaluator e-1\n✔ Deleted evaluator e-2\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := runBatchDelete(t, newIOForTest(t, ""), tc.ids, tc.del)
			require.NoError(t, err)
			// Pre-migration these commands wrote nothing to stdout on
			// success; the silent text codec must keep that byte-identical.
			assert.Empty(t, stdout, "default human stdout must stay empty")
			assert.Equal(t, tc.wantStderr, stderr)
		})
	}
}

func TestRunBatchDelete_AgentMode_SingleJSONDocument(t *testing.T) {
	withPlainColors(t)
	setAgentMode(t, true)

	stdout, _, err := runBatchDelete(t, newIOForTest(t, ""), []string{"e-1", "e-2"}, failOn())
	require.NoError(t, err)

	doc := decodeSingleJSONValue(t, stdout)
	assert.Equal(t, "gcx.mutation_batch", doc["type"])
	assert.Equal(t, "1", doc["schema_version"])
	assert.Equal(t, "deleted", doc["action"])
	assert.Equal(t, map[string]any{"succeeded": float64(2), "failed": float64(0)}, doc["summary"])
	assert.Equal(t, []any{}, doc["failures"], "failures must serialize as [] when nothing failed")
}

func TestRunBatchDelete_PartialFailure(t *testing.T) {
	withPlainColors(t)

	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human mode", agentMode: false},
		{name: "agent mode", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, tc.agentMode)

			stdout, stderr, err := runBatchDelete(t, newIOForTest(t, ""), []string{"e-1", "e-2", "e-3"}, failOn("e-2"))

			// The error must be an EmittedError carrying ExitPartialFailure:
			// the document on stdout is complete, and reportError must not
			// append a second one.
			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted, "partial failure must return *gcxerrors.EmittedError, got %T (%v)", err, err)
			assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)

			if tc.agentMode {
				doc := decodeSingleJSONValue(t, stdout)
				assert.Equal(t, "gcx.mutation_batch", doc["type"])
				assert.Equal(t, map[string]any{"succeeded": float64(1), "failed": float64(1), "skipped": float64(1)}, doc["summary"])
				failures, ok := doc["failures"].([]any)
				require.True(t, ok, "failures must be an array: %v", doc["failures"])
				require.Len(t, failures, 1)
				failure, ok := failures[0].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, map[string]any{"kind": "evaluator", "id": "e-2"}, failure["target"])
				assert.Equal(t, "boom", failure["error"])

				// The stderr diagnostic is a JSONL typed-class record.
				lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
				last := lines[len(lines)-1]
				var record map[string]any
				require.NoError(t, json.Unmarshal([]byte(last), &record), "agent-mode stderr warning must be JSONL: %q", stderr)
				assert.Equal(t, "warning", record["class"])
				assert.Equal(t, "deleting evaluator e-2: boom", record["summary"])
				return
			}

			assert.Empty(t, stdout, "human stdout must stay empty on partial failure")
			assert.Equal(t, "✔ Deleted evaluator e-1\nwarn: deleting evaluator e-2: boom\n", stderr)
		})
	}
}

func TestRunBatchDelete_TotalFailure_RawError(t *testing.T) {
	withPlainColors(t)

	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human mode", agentMode: false},
		{name: "agent mode", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, tc.agentMode)

			stdout, _, err := runBatchDelete(t, newIOForTest(t, ""), []string{"e-1", "e-2"}, failOn("e-1"))

			// Nothing succeeded and nothing was written: the raw error path
			// (single fused error document via reportError) is the honest
			// one, so no EmittedError and no stdout bytes here.
			require.Error(t, err)
			var emitted *gcxerrors.EmittedError
			require.NotErrorAs(t, err, &emitted, "total failure must not return EmittedError")
			require.EqualError(t, err, "deleting evaluator e-1: boom")
			assert.Empty(t, stdout, "stdout must stay empty when nothing was deleted")
		})
	}
}

func TestRunBatchDelete_ExplicitOutputOverride(t *testing.T) {
	withPlainColors(t)
	setAgentMode(t, false)

	t.Run("-o json", func(t *testing.T) {
		stdout, _, err := runBatchDelete(t, newIOForTest(t, "json"), []string{"e-1"}, failOn())
		require.NoError(t, err)
		doc := decodeSingleJSONValue(t, stdout)
		assert.Equal(t, "gcx.mutation_batch", doc["type"])
	})

	t.Run("-o yaml", func(t *testing.T) {
		stdout, _, err := runBatchDelete(t, newIOForTest(t, "yaml"), []string{"e-1"}, failOn())
		require.NoError(t, err)
		assert.Contains(t, stdout, "type: gcx.mutation_batch")
		assert.Contains(t, stdout, "action: deleted")
	})

	t.Run("explicit -o json wins in agent mode", func(t *testing.T) {
		setAgentMode(t, true)
		stdout, _, err := runBatchDelete(t, newIOForTest(t, "json"), []string{"e-1"}, failOn())
		require.NoError(t, err)
		doc := decodeSingleJSONValue(t, stdout)
		assert.Equal(t, "gcx.mutation_batch", doc["type"])
	})
}

func TestSilentTextCodec(t *testing.T) {
	codec := commandutil.SilentTextCodec{}
	assert.Equal(t, "text", string(codec.Format()))

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, cmdio.NewBatchMutation("deleted")))
	assert.Empty(t, buf.String(), "silent text codec must write zero bytes")

	require.Error(t, codec.Decode(nil, nil))
}

// TestRunBatchDelete_StopsAfterFailure pins the pre-migration stop-on-first-
// error loop semantics: ids after the failed one are never attempted.
func TestRunBatchDelete_StopsAfterFailure(t *testing.T) {
	withPlainColors(t)
	setAgentMode(t, false)

	var attempted []string
	del := func(id string) error {
		attempted = append(attempted, id)
		if id == "e-2" {
			return errors.New("boom")
		}
		return nil
	}

	_, _, err := runBatchDelete(t, newIOForTest(t, ""), []string{"e-1", "e-2", "e-3"}, del)
	require.Error(t, err)
	assert.Equal(t, []string{"e-1", "e-2"}, attempted, "the loop must stop at the first failure")
}
