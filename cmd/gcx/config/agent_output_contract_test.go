package config_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

// These tests pin the agent output contract for the config command family
// (#387): in agent mode with no explicit -o, every finite command emits
// exactly one JSON value on stdout; the human default output stays
// byte-identical to the pre-migration implementation; explicit -o always
// wins over the agent-mode default.

// setAgentMode toggles agent mode for the duration of the test. It must run
// BEFORE the command tree is constructed — the shared output flags resolve
// their default format at BindFlags time.
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

// runConfigCommand executes a fresh `gcx config ...` tree and returns stdout
// with stderr kept out of it (the shared runConfigCmd helper combines the two
// streams, which would corrupt one-JSON-value assertions with stderr
// diagnostics).
func runConfigCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := config.Command()
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

func asObject(t *testing.T, doc any) map[string]any {
	t.Helper()
	obj, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("document is %T, want object", doc)
	}
	return obj
}

const twoContextsConfig = `current-context: old
contexts:
  old: {}
  new: {}`

func TestConfigCommands_AgentModeSingleJSONDocument(t *testing.T) {
	tests := []struct {
		name   string
		args   func(t *testing.T) []string
		assert func(t *testing.T, doc any)
	}{
		{
			name: "current-context",
			args: func(t *testing.T) []string {
				t.Helper()
				return []string{"current-context", "--config", "testdata/config.yaml"}
			},
			assert: func(t *testing.T, doc any) {
				t.Helper()
				obj := asObject(t, doc)
				require.Equal(t, "local", obj["current-context"])
			},
		},
		{
			name: "list-contexts",
			args: func(t *testing.T) []string {
				t.Helper()
				return []string{"list-contexts", "--config", "testdata/config.yaml"}
			},
			assert: func(t *testing.T, doc any) {
				t.Helper()
				obj := asObject(t, doc)
				contexts, ok := obj["contexts"].([]any)
				require.True(t, ok, "contexts must be an array, got %T", obj["contexts"])
				require.Len(t, contexts, 2)
				first := asObject(t, contexts[0])
				require.Equal(t, "local", first["name"])
				require.Equal(t, true, first["current"])
			},
		},
		{
			name: "set",
			args: func(t *testing.T) []string {
				t.Helper()
				configFile := testutils.CreateTempFile(t, "version: 1\ncurrent-context: dev")
				return []string{"set", "--config", configFile, "stacks.dev.grafana.server", "https://example.test"}
			},
			assert: func(t *testing.T, doc any) {
				t.Helper()
				obj := asObject(t, doc)
				require.Equal(t, "gcx.config.mutation", obj["type"])
				require.Equal(t, "1", obj["schema_version"])
				require.Equal(t, "set", obj["action"])
				require.Equal(t, "stacks.dev.grafana.server", obj["property"])
				require.NotEmpty(t, obj["file"])
			},
		},
		{
			name: "unset",
			args: func(t *testing.T) []string {
				t.Helper()
				configFile := testutils.CreateTempFile(t, `version: 1
current-context: dev
stacks:
  dev:
    grafana:
      server: https://example.test`)
				return []string{"unset", "--config", configFile, "stacks.dev.grafana.server"}
			},
			assert: func(t *testing.T, doc any) {
				t.Helper()
				obj := asObject(t, doc)
				require.Equal(t, "gcx.config.mutation", obj["type"])
				require.Equal(t, "unset", obj["action"])
				require.Equal(t, "stacks.dev.grafana.server", obj["property"])
			},
		},
		{
			name: "use-context",
			args: func(t *testing.T) []string {
				t.Helper()
				t.Setenv("XDG_STATE_HOME", t.TempDir())
				configFile := testutils.CreateTempFile(t, twoContextsConfig)
				return []string{"use-context", "--config", configFile, "new"}
			},
			assert: func(t *testing.T, doc any) {
				t.Helper()
				obj := asObject(t, doc)
				require.Equal(t, "gcx.mutation", obj["type"])
				require.Equal(t, "use-context", obj["action"])
				require.Equal(t, true, obj["changed"])
				target := asObject(t, obj["target"])
				require.Equal(t, "context", target["kind"])
				require.Equal(t, "new", target["name"])
			},
		},
		{
			// Idempotent no-op: the target context is already current, so the
			// document must report changed=false (not omit the field, and not
			// error) with a clean exit.
			name: "use-context no-op",
			args: func(t *testing.T) []string {
				t.Helper()
				t.Setenv("XDG_STATE_HOME", t.TempDir())
				configFile := testutils.CreateTempFile(t, twoContextsConfig)
				return []string{"use-context", "--config", configFile, "old"}
			},
			assert: func(t *testing.T, doc any) {
				t.Helper()
				obj := asObject(t, doc)
				require.Equal(t, "gcx.mutation", obj["type"])
				require.Equal(t, "use-context", obj["action"])
				require.Equal(t, false, obj["changed"])
				target := asObject(t, obj["target"])
				require.Equal(t, "context", target["kind"])
				require.Equal(t, "old", target["name"])
			},
		},
		{
			name: "path",
			args: func(t *testing.T) []string {
				t.Helper()
				_, workDir := isolatedConfigEnv(t)
				writeLocalConfig(t, workDir, "contexts: {}\n")
				return []string{"path"}
			},
			assert: func(t *testing.T, doc any) {
				t.Helper()
				entries, ok := doc.([]any)
				require.True(t, ok, "path result must be an array, got %T", doc)
				require.Len(t, entries, 1)
				entry := asObject(t, entries[0])
				require.Equal(t, "local", entry["type"])
				require.NotEmpty(t, entry["path"])
			},
		},
		{
			// LoadLayered auto-creates the user config when none exist, so the
			// result reports it — the "No config files found." branch is only
			// reachable when auto-creation cannot repopulate the sources.
			name: "path auto-creates and reports the user config",
			args: func(t *testing.T) []string {
				t.Helper()
				isolatedConfigEnv(t)
				return []string{"path"}
			},
			assert: func(t *testing.T, doc any) {
				t.Helper()
				entries, ok := doc.([]any)
				require.True(t, ok, "path result must be an array, got %T", doc)
				require.Len(t, entries, 1)
				entry := asObject(t, entries[0])
				require.Equal(t, "user", entry["type"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := tc.args(t)
			setAgentMode(t, true)

			stdout, err := runConfigCommand(t, args...)
			require.NoError(t, err, stdout)

			tc.assert(t, decodeSingleJSONValue(t, stdout))
		})
	}
}

func TestConfigCommands_HumanDefaultByteIdentical(t *testing.T) {
	expectedContextsTable := func(t *testing.T) string {
		t.Helper()
		// Same table calls as the pre-migration implementation, for the
		// testdata/config.yaml contexts in sorted order.
		var buf bytes.Buffer
		require.NoError(t, style.NewTable("CURRENT", "NAME", "GRAFANA SERVER").
			Row("*", "local", "http://localhost:3000/").
			Row(" ", "prod", "https://grafana.example.com/").
			Render(&buf))
		return buf.String()
	}

	tests := []struct {
		name     string
		args     func(t *testing.T) []string
		expected func(t *testing.T) string
	}{
		{
			name: "current-context prints the bare name",
			args: func(t *testing.T) []string {
				t.Helper()
				return []string{"current-context", "--config", "testdata/config.yaml"}
			},
			expected: func(t *testing.T) string { t.Helper(); return "local\n" },
		},
		{
			name: "list-contexts renders the table",
			args: func(t *testing.T) []string {
				t.Helper()
				return []string{"list-contexts", "--config", "testdata/config.yaml"}
			},
			expected: expectedContextsTable,
		},
		{
			name: "set prints nothing on success",
			args: func(t *testing.T) []string {
				t.Helper()
				configFile := testutils.CreateTempFile(t, "version: 1\ncurrent-context: dev")
				return []string{"set", "--config", configFile, "stacks.dev.grafana.server", "https://example.test"}
			},
			expected: func(t *testing.T) string { t.Helper(); return "" },
		},
		{
			name: "unset prints nothing on success",
			args: func(t *testing.T) []string {
				t.Helper()
				configFile := testutils.CreateTempFile(t, `version: 1
current-context: dev
stacks:
  dev:
    grafana:
      server: https://example.test`)
				return []string{"unset", "--config", configFile, "stacks.dev.grafana.server"}
			},
			expected: func(t *testing.T) string { t.Helper(); return "" },
		},
		{
			name: "use-context prints the confirmation line",
			args: func(t *testing.T) []string {
				t.Helper()
				t.Setenv("XDG_STATE_HOME", t.TempDir())
				configFile := testutils.CreateTempFile(t, twoContextsConfig)
				return []string{"use-context", "--config", configFile, "new"}
			},
			expected: func(t *testing.T) string { t.Helper(); return "✔ Context set to \"new\"\n" },
		},
		{
			name: "use-context no-op prints the already-set line",
			args: func(t *testing.T) []string {
				t.Helper()
				t.Setenv("XDG_STATE_HOME", t.TempDir())
				configFile := testutils.CreateTempFile(t, twoContextsConfig)
				return []string{"use-context", "--config", configFile, "old"}
			},
			expected: func(t *testing.T) string { t.Helper(); return "✔ Context already set to \"old\"\n" },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			disableColor(t)
			args := tc.args(t)
			setAgentMode(t, false)

			stdout, err := runConfigCommand(t, args...)
			require.NoError(t, err, stdout)
			require.Equal(t, tc.expected(t), stdout)
		})
	}
}

// TestConfigPath_HumanTableByteIdentical pins the non-empty table rendering
// against the same style.NewTable calls the pre-migration implementation
// made, values included.
func TestConfigPath_HumanTableByteIdentical(t *testing.T) {
	disableColor(t)
	_, workDir := isolatedConfigEnv(t)
	localPath := writeLocalConfig(t, workDir, "contexts: {}\n")
	setAgentMode(t, false)

	info, err := os.Stat(localPath)
	require.NoError(t, err)

	var expected bytes.Buffer
	require.NoError(t, style.NewTable("PRIORITY", "TYPE", "PATH", "MODIFIED").
		Row("1", "local", localPath, info.ModTime().Format(time.DateTime)).
		Render(&expected))

	stdout, err := runConfigCommand(t, "path")
	require.NoError(t, err)
	require.Equal(t, expected.String(), stdout)
}

func TestConfigCommands_ExplicitOutputOverride(t *testing.T) {
	t.Run("current-context -o yaml", func(t *testing.T) {
		setAgentMode(t, false)
		stdout, err := runConfigCommand(t, "current-context", "--config", "testdata/config.yaml", "-o", "yaml")
		require.NoError(t, err)
		require.Equal(t, "current-context: local\n", stdout)
	})

	t.Run("current-context -o json wins in agent mode", func(t *testing.T) {
		setAgentMode(t, true)
		stdout, err := runConfigCommand(t, "current-context", "--config", "testdata/config.yaml", "-o", "json")
		require.NoError(t, err)
		obj := asObject(t, decodeSingleJSONValue(t, stdout))
		require.Equal(t, "local", obj["current-context"])
		// The builtin json codec pretty-prints; the agents codec is compact.
		// Indentation proves the explicit -o json flag beat the agents default.
		require.Contains(t, stdout, "\n  \"current-context\"")
	})

	t.Run("use-context -o json", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		configFile := testutils.CreateTempFile(t, twoContextsConfig)
		setAgentMode(t, false)

		stdout, err := runConfigCommand(t, "use-context", "--config", configFile, "new", "-o", "json")
		require.NoError(t, err)
		obj := asObject(t, decodeSingleJSONValue(t, stdout))
		require.Equal(t, "gcx.mutation", obj["type"])
		require.Equal(t, true, obj["changed"])
	})

	t.Run("set -o yaml", func(t *testing.T) {
		configFile := testutils.CreateTempFile(t, "version: 1\ncurrent-context: dev")
		setAgentMode(t, false)

		stdout, err := runConfigCommand(t, "set", "--config", configFile, "stacks.dev.grafana.server", "https://example.test", "-o", "yaml")
		require.NoError(t, err)
		require.Contains(t, stdout, "action: set")
		require.Contains(t, stdout, "property: stacks.dev.grafana.server")
	})

	t.Run("path -o json stays a pretty-printed array", func(t *testing.T) {
		_, workDir := isolatedConfigEnv(t)
		writeLocalConfig(t, workDir, "contexts: {}\n")
		setAgentMode(t, false)

		stdout, err := runConfigCommand(t, "path", "-o", "json")
		require.NoError(t, err)
		entries, ok := decodeSingleJSONValue(t, stdout).([]any)
		require.True(t, ok, "expected JSON array, got:\n%s", stdout)
		require.Len(t, entries, 1)
		require.True(t, strings.HasPrefix(stdout, "[\n"), "expected pretty-printed array, got:\n%s", stdout)
	})

	t.Run("unknown -o is rejected", func(t *testing.T) {
		setAgentMode(t, false)
		_, err := runConfigCommand(t, "current-context", "--config", "testdata/config.yaml", "-o", "bogus")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown output format")
	})
}

// TestConfigCheck_ExitCodeAgreesWithFindings pins the separable exit-code fix
// for `config check`: a context that fails validation must produce a non-zero
// exit. The prose report is the command's complete output, so the failure is
// an AlreadyReportedError — reportError exits with the code without appending
// a second (error) document to the stream.
func TestConfigCheck_ExitCodeAgreesWithFindings(t *testing.T) {
	configFile := testutils.CreateTempFile(t, `version: 1
contexts:
  dev: {}
current-context: dev`)

	testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"check", "--config", configFile},
		Assertions: []testutils.CommandAssertion{
			func(t *testing.T, result testutils.CommandResult) {
				t.Helper()
				code, reported := gcxerrors.AlreadyReportedExitCode(result.Err)
				require.True(t, reported, "check failure must carry the already-reported sentinel, got %v", result.Err)
				require.Equal(t, gcxerrors.ExitGeneralError, code)
				// The prose report still carries the finding and its context.
				require.Contains(t, result.Stdout, "context references no stack")
				require.Contains(t, result.Stdout, "Connectivity:")
			},
		},
	}.Run(t)
}

// TestConfigEdit_AgentModeGuard pins the prompt-without-guard fix: in agent
// mode `config edit` must never exec $EDITOR (which would hang against a
// pipe); it returns a structured error naming the scripted alternatives.
func TestConfigEdit_AgentModeGuard(t *testing.T) {
	setAgentMode(t, true)

	stdout, err := runConfigCommand(t, "edit")
	require.Error(t, err)

	var detailed gcxerrors.DetailedError
	require.ErrorAs(t, err, &detailed)
	require.Equal(t, "interactive editor disabled in agent mode", detailed.Summary)
	require.NotEmpty(t, detailed.Suggestions)
	require.Empty(t, stdout, "the guard must fire before anything reaches stdout")
}
