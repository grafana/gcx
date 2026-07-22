package root_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Agent output contract conformance smoke test (docs/design/agent-mode.md):
// runs a freshly built gcx binary against offline-runnable commands and
// asserts, per protocol class:
//
//   - finite: stdout parses as EXACTLY ONE JSON value followed by EOF —
//     on success and on failure (in-band error), with the exit code
//     agreeing with the outcome;
//   - explicit -o overrides win over the agent-mode default;
//   - stdin is closed for every invocation, so any surviving interactive
//     prompt hangs and fails the test by timeout rather than passing
//     silently.
//
// Backend-dependent commands are covered by per-package tests with fakes;
// this suite pins the end-to-end wiring (BindFlags override, reportError,
// EmittedError suppression) that unit tests cannot see.

var (
	buildOnce sync.Once //nolint:gochecknoglobals // shared across subtests
	buildPath string    //nolint:gochecknoglobals
	errBuild  error     //nolint:gochecknoglobals
)

// buildGcx builds the gcx binary once per test run — always fresh, never
// trusting a stale bin/gcx.
func buildGcx(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		// Not t.TempDir(): that directory is deleted when the first test to
		// build finishes, while sync.Once keeps serving the path to later
		// tests — they would run a vanished binary.
		dir, err := os.MkdirTemp("", "gcx-conformance-*") //nolint:usetesting // see above
		if err != nil {
			errBuild = err
			return
		}
		bin := filepath.Join(dir, "gcx")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		// Repo root is two levels up from cmd/gcx/root.
		cmd := exec.CommandContext(context.Background(), "go", "build", "-buildvcs=false", "-o", bin, "../../../cmd/gcx/")
		out, err := cmd.CombinedOutput()
		if err != nil {
			errBuild = errors.New("building gcx: " + err.Error() + "\n" + string(out))
			return
		}
		buildPath = bin
	})
	if errBuild != nil {
		t.Fatalf("%v", errBuild)
	}
	return buildPath
}

// runGcx runs the built binary with agent mode enabled, an isolated HOME and
// XDG environment, telemetry off, and stdin closed. It returns stdout and the
// exit code; stderr is captured only to keep it out of stdout.
func runGcx(t *testing.T, args ...string) (string, int) {
	t.Helper()
	bin := buildGcx(t)

	home := t.TempDir()
	cmd := exec.CommandContext(context.Background(), bin, args...)
	cmd.Env = []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".state"),
		"PATH=" + os.Getenv("PATH"),
		"GCX_AGENT_MODE=1",
		"GCX_TELEMETRY=off",
		"DO_NOT_TRACK=1",
	}
	cmd.Stdin = nil // exec: /dev/null — a surviving prompt reads EOF, never blocks on us

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running gcx %v: %v", args, err)
	}
	return outBuf.String(), code
}

// assertOneJSONValue decodes stdout and fails unless it holds exactly one
// JSON value followed by EOF.
func assertOneJSONValue(t *testing.T, stdout string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var first any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("stdout is not a JSON value: %v\nstdout:\n%s", err, stdout)
	}
	var second any
	if err := dec.Decode(&second); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout must contain exactly one JSON value; second decode = %v\nstdout:\n%s", err, stdout)
	}
	return first
}

func TestAgentConformance_FiniteCommandsEmitOneJSONValue(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the gcx binary; skipped with -short")
	}

	// Offline-runnable finite commands: success paths.
	successCases := []struct {
		name string
		args []string
	}{
		{name: "providers list", args: []string{"providers", "list"}},
		{name: "commands catalog", args: []string{"commands"}},
		{name: "skills list", args: []string{"agent", "skills", "list"}},
		{name: "dev lint list-rules", args: []string{"dev", "lint", "list-rules"}},
	}
	for _, tc := range successCases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, code := runGcx(t, tc.args...)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0\nstdout:\n%s", code, stdout)
			}
			assertOneJSONValue(t, stdout)
		})
	}
}

func TestAgentConformance_FailuresAreOneInBandErrorDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the gcx binary; skipped with -short")
	}

	// A command that fails without a backend: no config context exists in
	// the isolated HOME, so a server-dependent command fails fast. The
	// contract: stdout still carries exactly one JSON value (the in-band
	// error document with discriminators) and the exit code is non-zero.
	stdout, code := runGcx(t, "datasources", "list")
	if code == 0 {
		t.Fatalf("expected non-zero exit code without a configured context\nstdout:\n%s", stdout)
	}
	doc := assertOneJSONValue(t, stdout)
	obj, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("error document is %T, want object", doc)
	}
	if obj["type"] != "gcx.error" {
		t.Fatalf("error document type = %v, want gcx.error", obj["type"])
	}
	errField, ok := obj["error"].(map[string]any)
	if !ok {
		t.Fatal("error document missing error object")
	}
	exitCode, ok := errField["exitCode"].(float64)
	if !ok {
		t.Fatalf("in-band exitCode = %v (%T), want number", errField["exitCode"], errField["exitCode"])
	}
	if int(exitCode) != code {
		t.Fatalf("in-band exitCode %v disagrees with process exit code %d", errField["exitCode"], code)
	}
}

func TestAgentConformance_ExplicitOverrideWins(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the gcx binary; skipped with -short")
	}

	// Explicit -o yaml must override the agent-mode agents default: the
	// output must NOT parse as JSON (YAML mappings start with a key, not a
	// brace) — pinning "explicit flags are authoritative".
	stdout, code := runGcx(t, "providers", "list", "-o", "yaml")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s", code, stdout)
	}
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		t.Fatal("stdout empty, want YAML output")
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		t.Fatalf("explicit -o yaml produced JSON-shaped output — override not honored:\n%s", stdout)
	}
}
