//nolint:testpackage // white-box testing: drives commandWith and reuses fakeReporter.
package check

// These tests pin the pre-GA agent output contract for
// "gcx instrumentation check":
//
//   - agent mode emits EXACTLY ONE JSON value on stdout even when checks
//     fail (the fused result document carries the errors; the returned
//     *gcxerrors.EmittedError carries exit 4 and suppresses any second
//     document);
//   - default human stdout stays byte-identical to the pre-migration table;
//   - explicit -o json is honored;
//   - stray otel-checker prints to the process stdout are rerouted to
//     stderr, never interleaved with the result document.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	otelutils "github.com/grafana/otel-checker/checks/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingChecker returns a checker with one passing and one failing check.
func failingChecker() checker {
	return func(_ context.Context, _ otelutils.Commands) *otelutils.Reporter {
		return fakeReporter(
			map[string][]string{"SDK": {"OTEL_SERVICE_NAME is set"}},
			nil,
			map[string][]string{"Grafana Cloud": {"GRAFANA_CLOUD_INSTANCE_ID missing"}},
		)
	}
}

// executeCheck runs the check command built around c and returns stdout,
// stderr, and the Execute error.
func executeCheck(t *testing.T, c checker, args ...string) (string, string, error) {
	t.Helper()
	cmd := commandWith(c)
	// Mirror production: the gcx root command silences cobra's own error and
	// usage rendering (reportError owns it).
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"sdk", "--language=go"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestCheck_AgentMode_FailedChecks_OneDocumentAndExitPartialFailure(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	stdout, stderr, err := executeCheck(t, failingChecker())

	// The returned error must be the emitted sentinel with exit 4 — the
	// result document already carries the failing checks, and reportError
	// must not write a second document.
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
	assert.Contains(t, err.Error(), "1 check(s) failed")

	// EmittedError suppresses reportError's stderr rendering, so the
	// failure count diagnostic must be emitted explicitly (typed JSONL in
	// agent mode).
	assert.Contains(t, stderr, "1 check(s) failed",
		"stderr must carry the failure count diagnostic")

	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "agent stdout is not valid JSON:\n%s", stdout)
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF,
		"stdout must contain exactly one JSON value:\n%s", stdout)

	errsField, ok := doc["errors"].([]any)
	require.True(t, ok, "result document must enumerate failing checks: %v", doc)
	assert.Len(t, errsField, 1)
}

func TestCheck_AgentMode_AllPassing_SingleDocumentExitZero(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	passing := func(_ context.Context, _ otelutils.Commands) *otelutils.Reporter {
		return fakeReporter(map[string][]string{"SDK": {"ok"}}, nil, nil)
	}
	stdout, _, err := executeCheck(t, passing)
	require.NoError(t, err)

	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc))
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF)
}

// TestCheck_HumanDefault_ByteIdenticalTable verifies the default human stdout
// is exactly the table the pre-migration implementation rendered (the same
// CheckTableCodec output), with nothing appended after it.
func TestCheck_HumanDefault_ByteIdenticalTable(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	stdout, _, err := executeCheck(t, failingChecker())

	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted, "failed checks must exit non-zero in human mode too")

	var want bytes.Buffer
	results := runWith(context.Background(), otelutils.Commands{}, failingChecker(), io.Discard)
	require.NoError(t, (&CheckTableCodec{}).Encode(&want, results))

	assert.Equal(t, want.String(), stdout, "default human stdout must stay byte-identical to the table codec output")
}

func TestCheck_ExplicitJSONOverride(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	stdout, _, err := executeCheck(t, failingChecker(), "-o", "json")

	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "-o json stdout must be valid JSON:\n%s", stdout)
}

// TestCheck_StrayLibraryStdout_RedirectedToStderr verifies that anything the
// otel-checker library prints straight to the process stdout (e.g. Java/Maven
// parse failures) is rerouted to stderr and never contaminates the result
// document.
func TestCheck_StrayLibraryStdout_RedirectedToStderr(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	noisy := func(_ context.Context, _ otelutils.Commands) *otelutils.Reporter {
		// Must write to the process-global os.Stdout (as otel-checker
		// maven.go does) so the capture pipe is actually exercised.
		fmt.Fprintln(os.Stdout, "Error parsing JSON: unexpected token")
		return fakeReporter(map[string][]string{"SDK": {"ok"}}, nil, nil)
	}

	stdout, stderr, err := executeCheck(t, noisy)
	require.NoError(t, err)

	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stray library prints must not contaminate stdout:\n%s", stdout)
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF)

	assert.NotContains(t, stdout, "Error parsing JSON")
	assert.Contains(t, stderr, "Error parsing JSON: unexpected token",
		"the stray print must surface on stderr as a diagnostic")
}
