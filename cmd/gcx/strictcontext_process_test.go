package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	strictContextProcessHelper = "GCX_STRICT_CONTEXT_PROCESS_HELPER"
	strictContextArgsEnv       = "GCX_STRICT_CONTEXT_PROCESS_ARGS"

	// strictRefusalDetail is the part of the refusal Details that identifies a
	// strict-mode refusal; the Summary is the generic "Invalid command usage".
	strictRefusalDetail = "GCX_REQUIRE_CONTEXT is set, so gcx will not fall back to current-context"

	strictCaseTimeout = 20 * time.Second
	strictUnreachable = "http://127.0.0.1:1"
)

const strictContextConfig = `version: 1
stacks:
  inherited:
    grafana:
      server: http://127.0.0.1:1
      org-id: 1
      auth-method: token
      token: x
  selected:
    grafana:
      server: http://127.0.0.1:1
      org-id: 1
      auth-method: token
      token: x
contexts:
  inherited:
    stack: inherited
  selected:
    stack: selected
current-context: inherited
`

// TestStrictContextProcessHelper is the child process: it runs the real main().
func TestStrictContextProcessHelper(_ *testing.T) {
	if os.Getenv(strictContextProcessHelper) != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv(strictContextArgsEnv)), &args); err != nil {
		os.Stderr.WriteString("helper: undecodable args: " + err.Error() + "\n")
		os.Exit(99)
	}
	os.Args = append([]string{"gcx"}, args...)
	main()
}

type strictCase struct {
	name      string
	args      []string
	env       []string
	noConfig  bool // leave GCX_CONFIG unset and write no file
	agentMode bool
}

type strictResult struct {
	stdout, stderr string
	exitCode       int
}

func (r strictResult) refused() bool {
	return r.exitCode == 2 && strings.Contains(r.stdout+r.stderr, strictRefusalDetail)
}

// strictBaseEnv is os.Environ() without the variables that would select a
// context or overlay a stack. They are removed rather than set empty, because
// an empty GRAFANA_SERVER still overlays the config file's server.
func strictBaseEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if key == "GCX_CONTEXT" || key == "CONTEXT_NAME" || strings.HasPrefix(key, "GRAFANA_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func runStrictContext(t *testing.T, tc strictCase) strictResult {
	t.Helper()

	configPath := ""
	if !tc.noConfig {
		configPath = filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(configPath, []byte(strictContextConfig), 0o600))
	}
	encoded, err := json.Marshal(tc.args)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), strictCaseTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStrictContextProcessHelper$") //nolint:gosec
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = nil
	agentMode := "false"
	if tc.agentMode {
		agentMode = "true"
	}
	// Later entries win, so the per-case values follow the scrubbed base env.
	cmd.Env = append(strictBaseEnv(),
		strictContextProcessHelper+"=1",
		strictContextArgsEnv+"="+string(encoded),
		"GCX_REQUIRE_CONTEXT=true",
		"GCX_AGENT_MODE="+agentMode,
		"GCX_TELEMETRY=disabled",
		"GCX_NO_UPDATE_NOTIFIER=1",
		"NO_COLOR=1",
		"HOME="+t.TempDir(),
		"XDG_CONFIG_HOME="+t.TempDir(),
		"XDG_CONFIG_DIRS="+t.TempDir(),
		"XDG_CACHE_HOME="+t.TempDir(),
		"XDG_STATE_HOME="+t.TempDir(),
		"GCX_CONFIG="+configPath,
	)
	cmd.Env = append(cmd.Env, tc.env...)

	runErr := cmd.Run()
	require.NoError(t, ctx.Err(), "case timed out; stdout=%s stderr=%s", stdout.String(), stderr.String())
	res := strictResult{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exitErr):
		res.exitCode = exitErr.ExitCode()
	default:
		require.NoError(t, runErr)
	}
	return res
}

func TestStrictContext_Refused(t *testing.T) {
	cases := []strictCase{
		{name: "api", args: []string{"api", "/api/health"}},
		{name: "help=false long", args: []string{"--help=false", "api", "/api/health"}},
		{name: "help=false short", args: []string{"-h=false", "api", "/api/health"}},
		{name: "shadowed context", args: []string{"--context", "selected", "api", "/api/health", "--context="}},
		{name: "cloud stacks list", args: []string{"cloud", "stacks", "list", "--org", "x"}, env: []string{"GRAFANA_SERVER=https://x.grafana.net"}},
		{name: "resources list-examples", args: []string{"resources", "list-examples"}},
		{name: "commands --validate", args: []string{"commands", "--validate"}},
		{name: "config check", args: []string{"config", "check"}},
		{name: "login", args: []string{"login", ""}},
		{name: "login with GCX_CONTEXT only", args: []string{"login"}, env: []string{"GCX_CONTEXT=selected"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runStrictContext(t, tc)
			assert.Equal(t, 2, res.exitCode, "stdout=%s stderr=%s", res.stdout, res.stderr)
			assert.Contains(t, res.stdout+res.stderr, strictRefusalDetail)
		})
	}
}

// TestStrictContext_NoConfigFile covers first run (Review Focus 3). CONCERN:
// the brief expects a strict-mode refusal here, but the loader auto-creates a
// template whose "default" context has no stack and validates it before the
// selection check runs, so the user gets "Invalid configuration" (exit 1)
// instead of the refusal. This pins the current
// behaviour until the controller decides whether the selection check should run
// before validation.
func TestStrictContext_NoConfigFile(t *testing.T) {
	res := runStrictContext(t, strictCase{args: []string{"slo", "definitions", "list"}, noConfig: true})
	assert.Equal(t, 1, res.exitCode, "stdout=%s stderr=%s", res.stdout, res.stderr)
	assert.Contains(t, res.stdout+res.stderr, "context references no stack with grafana config")
	assert.NotContains(t, res.stdout+res.stderr, strictRefusalDetail)
}

func TestStrictContext_Refused_AgentMode(t *testing.T) {
	res := runStrictContext(t, strictCase{
		name:      "agent",
		args:      []string{"api", "/api/health"},
		agentMode: true,
	})
	require.Equal(t, 2, res.exitCode, "stdout=%s stderr=%s", res.stdout, res.stderr)

	var doc struct {
		Type  string `json:"type"`
		Error struct {
			Summary  string `json:"summary"`
			ExitCode int    `json:"exitCode"`
			Details  string `json:"details"`
		} `json:"error"`
	}
	dec := json.NewDecoder(strings.NewReader(res.stdout))
	require.NoError(t, dec.Decode(&doc), "stdout=%q", res.stdout)
	assert.False(t, dec.More(), "stdout must carry exactly one JSON document: %q", res.stdout)
	assert.Equal(t, "gcx.error", doc.Type)
	assert.Equal(t, 2, doc.Error.ExitCode)
	assert.Contains(t, doc.Error.Details, strictRefusalDetail)
}

func TestStrictContext_Accepted(t *testing.T) {
	cases := []strictCase{
		{name: "flag", args: []string{"--context", "selected", "api", "/api/health"}},
		{name: "env", args: []string{"api", "/api/health"}, env: []string{"GCX_CONTEXT=selected"}},
		{name: "config check with flag", args: []string{"config", "check", "--context", "selected"}},
		{
			name: "env-only setup", args: []string{"--context", "default", "api", "/api/health"},
			noConfig: true, env: []string{"GRAFANA_SERVER=" + strictUnreachable},
		},
		// login derives its target from GRAFANA_SERVER; stdin is not a TTY so it
		// must fail fast rather than prompt. Only the absence of a refusal matters.
		{name: "login with server", args: []string{"login", "--yes"}, env: []string{"GRAFANA_SERVER=" + strictUnreachable}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runStrictContext(t, tc)
			assert.NotContains(t, res.stdout+res.stderr, strictRefusalDetail, "exit=%d", res.exitCode)
		})
	}
}

func TestStrictContext_LocalCommandsNeverRefused(t *testing.T) {
	cases := [][]string{
		{"version"},
		{"help"},
		{"completion", "bash"},
		{"commands"},
		{"help-tree"},
		{"providers", "list"},
		{"docs", "list-products"},
		{"agent", "skills", "list"},
		{"config", "view"},
		{"config", "path"},
		{"config", "list-contexts"},
		{"config", "current-context"},
		{"config", "use-context", "selected"},
		{"dev", "lint", "list-rules"},
		{"instrumentation", "check"},
		{"instrumentation", "list-explanations"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			res := runStrictContext(t, strictCase{args: args})
			assert.False(t, res.refused(), "refused: exit=%d stdout=%s stderr=%s", res.exitCode, res.stdout, res.stderr)
			assert.NotContains(t, res.stdout+res.stderr, strictRefusalDetail)
		})
	}
}
