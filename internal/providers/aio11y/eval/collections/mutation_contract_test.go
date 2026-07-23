package collections //nolint:testpackage // Tests drive the unexported runDelete/emit* seams and opts wiring.

// Contract tests for the collections mutation verbs (#387 agent output
// contract): delete, add-conversations, remove-conversation. The shared
// batch-delete engine's edge cases live in commandutil; these tests pin the
// per-command wiring — silent human defaults, exact stderr receipts, the
// structured documents (gcx.mutation_batch for delete, the bespoke
// gcx.aio11y.collection_membership shape for the membership verbs), the
// partial-failure EmittedError, and explicit -o overrides.

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

func withPlainColors(t *testing.T) {
	t.Helper()
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })
}

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

func newMembershipOptsForTest(t *testing.T, output string) *membershipOpts {
	t.Helper()
	opts := &membershipOpts{}
	opts.IO.ErrWriter = io.Discard // silence the agent-mode --json/--jq hint
	flags := pflag.NewFlagSet("membership", pflag.ContinueOnError)
	opts.setup(flags)
	if output != "" {
		require.NoError(t, flags.Set("output", output))
	}
	require.NoError(t, opts.IO.Validate())
	return opts
}

func decodeSingleJSONValue(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", raw)
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF, "stdout must contain exactly one JSON value:\n%s", raw)
	return doc
}

func TestDelete_OutputContract(t *testing.T) {
	withPlainColors(t)

	del := func(fail string) func(string) error {
		return func(id string) error {
			if id == fail {
				return errors.New("boom")
			}
			return nil
		}
	}

	t.Run("human default stays byte-identical", func(t *testing.T) {
		agent.SetFlag(false)
		t.Cleanup(agent.ResetForTesting)

		opts := newDeleteOptsForTest(t, "")
		var stdout, stderr bytes.Buffer
		require.NoError(t, runDelete(&stdout, &stderr, opts, []string{"c-1", "c-2"}, del("")))
		assert.Empty(t, stdout.String(), "default human stdout must stay empty")
		assert.Equal(t, "✔ Deleted collection c-1\n✔ Deleted collection c-2\n", stderr.String())
	})

	t.Run("agent mode emits exactly one JSON value", func(t *testing.T) {
		agent.SetFlag(true)
		t.Cleanup(agent.ResetForTesting)

		opts := newDeleteOptsForTest(t, "")
		var stdout, stderr bytes.Buffer
		require.NoError(t, runDelete(&stdout, &stderr, opts, []string{"c-1"}, del("")))
		doc := decodeSingleJSONValue(t, stdout.String())
		assert.Equal(t, "gcx.mutation_batch", doc["type"])
		assert.Equal(t, "deleted", doc["action"])
	})

	t.Run("partial failure returns EmittedError with exit 4", func(t *testing.T) {
		agent.SetFlag(true)
		t.Cleanup(agent.ResetForTesting)

		opts := newDeleteOptsForTest(t, "")
		var stdout, stderr bytes.Buffer
		err := runDelete(&stdout, &stderr, opts, []string{"c-1", "c-2"}, del("c-2"))
		var emitted *gcxerrors.EmittedError
		require.ErrorAs(t, err, &emitted)
		assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
		decodeSingleJSONValue(t, stdout.String())
	})

	t.Run("explicit -o json and -o yaml override honored", func(t *testing.T) {
		agent.SetFlag(false)
		t.Cleanup(agent.ResetForTesting)

		var stdout, stderr bytes.Buffer
		require.NoError(t, runDelete(&stdout, &stderr, newDeleteOptsForTest(t, "json"), []string{"c-1"}, del("")))
		doc := decodeSingleJSONValue(t, stdout.String())
		assert.Equal(t, "gcx.mutation_batch", doc["type"])

		stdout.Reset()
		require.NoError(t, runDelete(&stdout, &stderr, newDeleteOptsForTest(t, "yaml"), []string{"c-1"}, del("")))
		assert.Contains(t, stdout.String(), "type: gcx.mutation_batch")
	})
}

func TestMembership_OutputContract(t *testing.T) {
	withPlainColors(t)

	tests := []struct {
		name       string
		agentMode  bool
		output     string
		emit       func(t *testing.T, stdout, stderr io.Writer, opts *membershipOpts) error
		wantStderr string
		wantDoc    map[string]any
	}{
		{
			name: "add human default stays byte-identical",
			emit: func(t *testing.T, stdout, stderr io.Writer, opts *membershipOpts) error {
				t.Helper()
				return emitAddConversationsReceipt(stdout, stderr, opts, "c-1", []string{"s-1", "s-2"})
			},
			wantStderr: "✔ Added 2 conversation(s) to collection c-1\n",
		},
		{
			name: "remove human default stays byte-identical",
			emit: func(t *testing.T, stdout, stderr io.Writer, opts *membershipOpts) error {
				t.Helper()
				return emitRemoveConversationReceipt(stdout, stderr, opts, "c-1", "s-1")
			},
			wantStderr: "✔ Removed s-1 from collection c-1\n",
		},
		{
			name:      "add agent mode emits exactly one JSON value",
			agentMode: true,
			emit: func(t *testing.T, stdout, stderr io.Writer, opts *membershipOpts) error {
				t.Helper()
				return emitAddConversationsReceipt(stdout, stderr, opts, "c-1", []string{"s-1", "s-2"})
			},
			wantDoc: map[string]any{
				"type":           "gcx.aio11y.collection_membership",
				"schema_version": "1",
				"action":         "added",
				"collection_id":  "c-1",
				"saved_ids":      []any{"s-1", "s-2"},
			},
		},
		{
			name:      "remove agent mode emits exactly one JSON value",
			agentMode: true,
			emit: func(t *testing.T, stdout, stderr io.Writer, opts *membershipOpts) error {
				t.Helper()
				return emitRemoveConversationReceipt(stdout, stderr, opts, "c-1", "s-1")
			},
			wantDoc: map[string]any{
				"type":           "gcx.aio11y.collection_membership",
				"schema_version": "1",
				"action":         "removed",
				"collection_id":  "c-1",
				"saved_ids":      []any{"s-1"},
			},
		},
		{
			name:   "add explicit -o json override honored",
			output: "json",
			emit: func(t *testing.T, stdout, stderr io.Writer, opts *membershipOpts) error {
				t.Helper()
				return emitAddConversationsReceipt(stdout, stderr, opts, "c-1", []string{"s-1"})
			},
			wantDoc: map[string]any{"type": "gcx.aio11y.collection_membership", "action": "added"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(agent.ResetForTesting)

			opts := newMembershipOptsForTest(t, tc.output)
			var stdout, stderr bytes.Buffer
			require.NoError(t, tc.emit(t, &stdout, &stderr, opts))

			if tc.wantDoc != nil {
				doc := decodeSingleJSONValue(t, stdout.String())
				for key, want := range tc.wantDoc {
					assert.Equal(t, want, doc[key], "field %q", key)
				}
				return
			}

			assert.Empty(t, stdout.String(), "default human stdout must stay empty")
			assert.Equal(t, tc.wantStderr, stderr.String())
		})
	}
}

func TestMembership_ExplicitYAMLOverride(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	opts := newMembershipOptsForTest(t, "yaml")
	var stdout, stderr bytes.Buffer
	require.NoError(t, emitRemoveConversationReceipt(&stdout, &stderr, opts, "c-1", "s-1"))
	assert.Contains(t, stdout.String(), "type: gcx.aio11y.collection_membership")
	assert.Contains(t, stdout.String(), "action: removed")
}
