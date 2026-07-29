//nolint:testpackage // white-box testing: drives the unexported command constructors and fake client.
package apps

// These tests pin the pre-GA agent output contract for the apps mutation
// commands (configure, remove):
//
//   - default human stdout stays byte-identical to the pre-migration
//     MutationResult.Emit one-liner;
//   - agent mode emits EXACTLY ONE JSON value (a
//     gcx.instrumentation.mutation document) on stdout, then EOF;
//   - explicit -o json / -o yaml overrides are honored;
//   - a post-write discovery-probe failure still emits the result document
//     (mutation applied → never a bare failure), with the probe error on
//     stderr.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// appsMutationCases is the shared table for the apps mutation commands. The
// fake starts with namespace "grotshop" (autoinstrument on, tracing off), so
// --tracing is a real change and --tracing=false is a no-op.
func appsMutationCases() []struct {
	name        string
	args        []string
	wantHuman   string
	wantAction  string
	wantChanged bool
} {
	return []struct {
		name        string
		args        []string
		wantHuman   string
		wantAction  string
		wantChanged bool
	}{
		{
			name:       "configure rmw change",
			args:       []string{"c1", "grotshop", "--tracing"},
			wantHuman:  "configure \"c1/grotshop\": done\n",
			wantAction: "configure", wantChanged: true,
		},
		{
			name:       "configure rmw no-op",
			args:       []string{"c1", "grotshop", "--tracing=false"},
			wantHuman:  "configure \"c1/grotshop\": no changes\n",
			wantAction: "configure", wantChanged: false,
		},
		{
			name:       "remove",
			args:       []string{"c1", "grotshop", "--yes"},
			wantHuman:  "remove \"c1/grotshop\": done\n",
			wantAction: "remove", wantChanged: true,
		},
	}
}

// runAppsMutation executes the configure or remove command end-to-end via
// cobra and returns stdout.
func runAppsMutation(t *testing.T, verb string, args []string, extraArgs ...string) (string, error) {
	t.Helper()

	client := &fakeAppsClient{
		getResponses: []getResponse{{namespaces: []instrumentation.App{
			{Name: "grotshop", Autoinstrument: new(true), Tracing: new(false)},
		}}},
	}

	cmd := newConfigureCmd(client)
	if verb == "remove" {
		cmd = newRemoveCmd(client, instrumentation.BackendURLs{})
	}

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append(append([]string{}, args...), extraArgs...))
	err := cmd.Execute()
	return stdout.String(), err
}

func verbOf(action string) string {
	if action == "remove" {
		return "remove"
	}
	return "configure"
}

func TestAppsMutations_HumanDefault_ByteIdentical(t *testing.T) {
	for _, tc := range appsMutationCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GCX_AGENT_MODE", "false")
			agent.ResetForTesting()
			t.Cleanup(agent.ResetForTesting)

			stdout, err := runAppsMutation(t, verbOf(tc.wantAction), tc.args)
			require.NoError(t, err)
			assert.Equal(t, tc.wantHuman, stdout, "default human stdout must stay byte-identical")
		})
	}
}

func TestAppsMutations_AgentMode_SingleJSONDocument(t *testing.T) {
	for _, tc := range appsMutationCases() {
		t.Run(tc.name, func(t *testing.T) {
			// The agents default is resolved when the command binds its
			// flags, so the flag must be set before building the command.
			agent.SetFlag(true)
			t.Cleanup(func() { agent.SetFlag(false) })

			stdout, err := runAppsMutation(t, verbOf(tc.wantAction), tc.args)
			require.NoError(t, err)

			doc := decodeOneJSONValue(t, stdout)
			assert.Equal(t, instoutput.MutationResultType, doc["type"])
			assert.Equal(t, "1", doc["schema_version"])
			assert.Equal(t, tc.wantAction, doc["action"])
			assert.Equal(t, tc.wantChanged, doc["changed"])
			target, ok := doc["target"].(map[string]any)
			require.True(t, ok, "target must be an object")
			assert.Equal(t, "c1", target["cluster"])
			assert.Equal(t, "grotshop", target["namespace"])

			if tc.wantAction == "configure" {
				// configure enriches the result with the discovery probe.
				_, hasDiscovered := doc["discovered"]
				assert.True(t, hasDiscovered, "configure result must carry discovered")
			}
		})
	}
}

func TestAppsMutations_ExplicitOutputOverrides(t *testing.T) {
	t.Run("-o yaml wins over agent default", func(t *testing.T) {
		agent.SetFlag(true)
		t.Cleanup(func() { agent.SetFlag(false) })

		stdout, err := runAppsMutation(t, "configure", []string{"c1", "grotshop", "--tracing"}, "-o", "yaml")
		require.NoError(t, err)
		assert.Contains(t, stdout, "action: configure\n")
		assert.Contains(t, stdout, "type: gcx.instrumentation.mutation\n")
		assert.Contains(t, stdout, "namespace: grotshop\n")
	})

	t.Run("-o json in human mode", func(t *testing.T) {
		t.Setenv("GCX_AGENT_MODE", "false")
		agent.ResetForTesting()
		t.Cleanup(agent.ResetForTesting)

		stdout, err := runAppsMutation(t, "remove", []string{"c1", "grotshop", "--yes"}, "-o", "json")
		require.NoError(t, err)
		doc := decodeOneJSONValue(t, stdout)
		assert.Equal(t, "remove", doc["action"])
	})
}

// TestAppsConfigure_PostWriteDiscoveryFailure_StillEmitsResult verifies that
// when the mutation has been applied but the post-write discovery probe
// fails, the result document is still the single stdout value (without the
// discovered enrichment), the probe failure lands on stderr, and the command
// succeeds — an applied mutation is never reported as a bare failure.
func TestAppsConfigure_PostWriteDiscoveryFailure_StillEmitsResult(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	client := &fakeAppsClient{
		getResponses: []getResponse{{namespaces: []instrumentation.App{
			{Name: "grotshop", Autoinstrument: new(true), Tracing: new(false)},
		}}},
		discoverErr: errors.New("discovery backend unavailable"),
	}

	cmd := newConfigureCmd(client)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"c1", "grotshop", "--tracing"})

	require.NoError(t, cmd.Execute(), "an applied mutation must not surface the probe failure as the command error")
	require.NotEmpty(t, client.setCalls, "the mutation must have been applied")

	doc := decodeOneJSONValue(t, stdout.String())
	assert.Equal(t, instoutput.MutationResultType, doc["type"])
	assert.Equal(t, true, doc["changed"])
	_, hasDiscovered := doc["discovered"]
	assert.False(t, hasDiscovered, "discovered must be omitted when the probe failed")

	assert.Contains(t, stderr.String(), "discovery backend unavailable",
		"the probe failure must surface on stderr")
}

// TestAppsConfigure_PreWriteDiscoveryFailure_ReturnsError verifies the
// pre-write no-op path keeps returning the probe error: nothing was mutated,
// so the error document is the honest terminal result (single doc, no
// partial stdout write).
func TestAppsConfigure_PreWriteDiscoveryFailure_ReturnsError(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	client := &fakeAppsClient{
		getResponses: []getResponse{{namespaces: []instrumentation.App{
			{Name: "grotshop", Autoinstrument: new(true), Tracing: new(false)},
		}}},
		discoverErr: errors.New("discovery backend unavailable"),
	}

	cmd := newConfigureCmd(client)
	// Mirror production: the gcx root command silences cobra's own error and
	// usage rendering (reportError owns it), so stdout stays untouched.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"c1", "grotshop", "--tracing=false"}) // no-op: tracing already false

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discovery backend unavailable")
	assert.Empty(t, client.setCalls, "no mutation may be applied on the no-op path")
	assert.Empty(t, stdout.String(), "no result document may be written when the command errors")
}

// decodeOneJSONValue asserts that raw holds exactly one JSON value followed
// by EOF, and returns it.
func decodeOneJSONValue(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", raw)
	var second any
	err := dec.Decode(&second)
	require.ErrorIs(t, err, io.EOF, "stdout must contain exactly one JSON value:\n%s", raw)
	return doc
}
