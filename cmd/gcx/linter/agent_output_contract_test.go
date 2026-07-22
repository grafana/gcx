//nolint:testpackage // white-box: the lint/test/new subcommands are unexported
package linter

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/linter"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the agent output contract for the linter command family
// (#387): in agent mode with no explicit -o, every finite command emits
// exactly one JSON value on stdout; the human default output stays
// byte-identical to the pre-migration implementation; explicit -o always
// wins over the agent-mode default; violations/failed tests carry a
// non-zero exit code via EmittedError without a second stdout document.

// setAgentMode toggles agent mode for the duration of the test. It must run
// BEFORE the command is constructed — the shared output flags resolve their
// default format at BindFlags time.
func setAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	agent.SetFlag(enabled)
	t.Cleanup(func() { agent.SetFlag(false) })
}

// disableColor forces deterministic plain rendering for byte-identical
// assertions regardless of the environment the tests run in.
func disableColor(t *testing.T) {
	t.Helper()
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })
}

// runCommand executes the command and returns stdout (stderr is captured
// separately so diagnostics never corrupt one-JSON-value assertions).
func runCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var stdout, stderr bytes.Buffer
	cmd.SetIn(&bytes.Buffer{})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), err
}

// decodeSingleJSONValue asserts stdout holds exactly one JSON value followed
// by EOF, and returns it.
func decodeSingleJSONValue(t *testing.T, stdout string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var first any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout:\n%s", err, stdout)
	}
	var second any
	if err := dec.Decode(&second); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout must contain exactly one JSON value, second decode = %v\nstdout:\n%s", err, stdout)
	}
	return first
}

func requireEmitted(t *testing.T, err error, wantCode int) *gcxerrors.EmittedError {
	t.Helper()
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted, "want *gcxerrors.EmittedError, got %T (%v)", err, err)
	require.Equal(t, wantCode, emitted.Code)
	return emitted
}

// writeRegoTestFixture writes a rego test file into a temp dir and returns
// the dir. With failing=true the suite contains one passing and one failing
// test case.
func writeRegoTestFixture(t *testing.T, failing bool) string {
	t.Helper()
	dir := t.TempDir()
	content := "package example\n\ntest_passes if {\n\t1 + 1 == 2\n}\n"
	if failing {
		content += "\ntest_fails if {\n\t1 == 2\n}\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "example_test.rego"), []byte(content), 0o600))
	return dir
}

func TestLintRun_HumanDefaultByteIdentical(t *testing.T) {
	disableColor(t)
	setAgentMode(t, false)

	stdout, err := runCommand(t, lintCmd(), "testdata/valid.json")
	require.NoError(t, err)

	// Exact pre-migration pretty output for a clean report.
	require.Equal(t, "1 file linted. No violations found.\n", stdout)
}

func TestLintRun_ViolationsExitCode(t *testing.T) {
	disableColor(t)

	tests := []struct {
		name      string
		agentMode bool
		args      []string
	}{
		{name: "human pretty default", agentMode: false, args: []string{"testdata/missing-panel-title.json"}},
		{name: "agent default", agentMode: true, args: []string{"testdata/missing-panel-title.json"}},
		{name: "explicit -o compact", agentMode: false, args: []string{"-o", "compact", "testdata/missing-panel-title.json"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, tc.agentMode)

			stdout, err := runCommand(t, lintCmd(), tc.args...)

			// Violations must carry exit 4 without a second stdout document.
			emitted := requireEmitted(t, err, gcxerrors.ExitPartialFailure)
			var partial *gcxerrors.PartialFailureError
			require.ErrorAs(t, emitted, &partial)

			if tc.agentMode {
				doc := decodeSingleJSONValue(t, stdout)
				obj, ok := doc.(map[string]any)
				require.True(t, ok, "agent-mode document is %T, want object", doc)
				require.Contains(t, obj, "violations")
				require.Contains(t, obj, "summary")
			} else {
				assert.Contains(t, stdout, "panel-title-description")
			}
		})
	}
}

func TestLintRun_AgentModeSingleJSONDocument(t *testing.T) {
	setAgentMode(t, true)

	stdout, err := runCommand(t, lintCmd(), "testdata/valid.json")
	require.NoError(t, err)

	doc := decodeSingleJSONValue(t, stdout)
	obj, ok := doc.(map[string]any)
	require.True(t, ok, "document is %T, want object", doc)
	summary, ok := obj["summary"].(map[string]any)
	require.True(t, ok, "summary is %T, want object", obj["summary"])
	require.InDelta(t, 1, summary["files_scanned"], 0)
	require.InDelta(t, 0, summary["num_violations"], 0)
}

func TestLintRun_ExplicitOutputOverridesAgentDefault(t *testing.T) {
	setAgentMode(t, true)

	stdout, err := runCommand(t, lintCmd(), "-o", "yaml", "testdata/valid.json")
	require.NoError(t, err)

	assert.Contains(t, stdout, "files_scanned: 1")
	assert.NotContains(t, stdout, "{")
}

func TestLintTest_HumanDefaultByteIdentical(t *testing.T) {
	disableColor(t)
	setAgentMode(t, false)
	dir := writeRegoTestFixture(t, false)

	stdout, err := runCommand(t, testCmd(), dir)
	require.NoError(t, err)

	// Exact pre-migration OPA pretty reporter output for an all-pass run.
	require.Equal(t, "PASS: 1/1\n", stdout)
}

func TestLintTest_AgentModeSingleJSONDocument(t *testing.T) {
	tests := []struct {
		name     string
		failing  bool
		wantErr  bool
		wantFail bool
	}{
		{name: "all tests pass", failing: false},
		{name: "failing tests keep one document and non-zero exit", failing: true, wantErr: true, wantFail: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, true)
			dir := writeRegoTestFixture(t, tc.failing)

			stdout, err := runCommand(t, testCmd(), dir)

			if tc.wantErr {
				emitted := requireEmitted(t, err, gcxerrors.ExitGeneralError)
				require.ErrorIs(t, emitted, linter.ErrTestsFailed)
			} else {
				require.NoError(t, err)
			}

			doc := decodeSingleJSONValue(t, stdout)
			results, ok := doc.([]any)
			require.True(t, ok, "document is %T, want array of test results", doc)
			require.NotEmpty(t, results)

			foundFail := false
			for _, r := range results {
				obj, ok := r.(map[string]any)
				require.True(t, ok)
				if fail, _ := obj["fail"].(bool); fail {
					foundFail = true
				}
			}
			require.Equal(t, tc.wantFail, foundFail)
		})
	}
}

func TestLintTest_ExplicitOutputOverrides(t *testing.T) {
	disableColor(t)

	t.Run("-o pretty wins in agent mode", func(t *testing.T) {
		setAgentMode(t, true)
		dir := writeRegoTestFixture(t, false)

		stdout, err := runCommand(t, testCmd(), "-o", "pretty", dir)
		require.NoError(t, err)
		require.Equal(t, "PASS: 1/1\n", stdout)
	})

	t.Run("-o json emits one document in human mode", func(t *testing.T) {
		setAgentMode(t, false)
		dir := writeRegoTestFixture(t, false)

		stdout, err := runCommand(t, testCmd(), "-o", "json", dir)
		require.NoError(t, err)
		doc := decodeSingleJSONValue(t, stdout)
		_, ok := doc.([]any)
		require.True(t, ok, "document is %T, want array", doc)
	})

	t.Run("-o yaml re-encodes the report", func(t *testing.T) {
		setAgentMode(t, false)
		dir := writeRegoTestFixture(t, false)

		stdout, err := runCommand(t, testCmd(), "-o", "yaml", dir)
		require.NoError(t, err)
		assert.Contains(t, stdout, "package: data.example")
	})
}

func TestLintTest_FailingTestsDirectFormats(t *testing.T) {
	disableColor(t)

	tests := []struct {
		name      string
		agentMode bool
		args      []string
	}{
		{name: "human pretty default", agentMode: false, args: nil},
		{name: "explicit -o json in agent mode", agentMode: true, args: []string{"-o", "json"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, tc.agentMode)
			dir := writeRegoTestFixture(t, true)

			stdout, err := runCommand(t, testCmd(), append(tc.args, dir)...)

			emitted := requireEmitted(t, err, gcxerrors.ExitGeneralError)
			require.ErrorIs(t, emitted, linter.ErrTestsFailed)
			assert.Contains(t, stdout, "test_fails")

			if len(tc.args) > 0 {
				// -o json: still exactly one JSON value even on failure — no
				// trailing error document.
				decodeSingleJSONValue(t, stdout)
			}
		})
	}
}

func TestLintNew_HumanDefaultByteIdentical(t *testing.T) {
	disableColor(t)
	setAgentMode(t, false)
	dir := t.TempDir()

	stdout, err := runCommand(t, newCmd(), "dashboard", "my-rule", "-o", dir)
	require.NoError(t, err)

	ruleFile := filepath.Join(dir, "rules", "custom", "gcx", "rules", "dashboard", "idiomatic", "my-rule", "my_rule.rego")
	require.Equal(t, "✔ Rule written in "+ruleFile+"\n", stdout)
	require.FileExists(t, ruleFile)
	require.FileExists(t, filepath.Join(filepath.Dir(ruleFile), "my_rule_test.rego"))
}

func TestLintNew_AgentModeArtifactReceipt(t *testing.T) {
	setAgentMode(t, true)
	dir := t.TempDir()

	stdout, err := runCommand(t, newCmd(), "dashboard", "my-rule", "-o", dir)
	require.NoError(t, err)

	doc := decodeSingleJSONValue(t, stdout)
	obj, ok := doc.(map[string]any)
	require.True(t, ok, "document is %T, want object", doc)
	require.Equal(t, "gcx.artifact_receipt", obj["type"])
	require.Equal(t, "1", obj["schema_version"])
	require.Equal(t, "scaffolded", obj["action"])
	files, ok := obj["files"].([]any)
	require.True(t, ok)
	require.Len(t, files, 2)
}

func TestLintNew_ErrorBeforeWriteEmitsNoDocument(t *testing.T) {
	setAgentMode(t, true)
	dir := t.TempDir()

	// First run creates the rule; second run must fail without stdout output.
	_, err := runCommand(t, newCmd(), "dashboard", "my-rule", "-o", dir)
	require.NoError(t, err)

	stdout, err := runCommand(t, newCmd(), "dashboard", "my-rule", "-o", dir)
	require.Error(t, err)
	var emitted *gcxerrors.EmittedError
	require.NotErrorAs(t, err, &emitted, "raw error expected: no result document was written")
	require.Empty(t, stdout)
}
