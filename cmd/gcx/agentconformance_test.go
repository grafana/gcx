package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	appversion "github.com/grafana/gcx/internal/version"
)

// Agent output contract conformance smoke test (docs/design/agent-mode.md):
// runs a gcx process helper against offline-runnable commands and
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
// Backend-dependent commands are covered by per-package tests with fakes.
// This suite pins the end-to-end wiring that unit tests cannot see.

const (
	agentConformanceProcessHelper = "GCX_AGENT_CONFORMANCE_PROCESS_HELPER"
	agentConformanceArgs          = "GCX_AGENT_CONFORMANCE_ARGS"
)

// TestAgentConformanceProcessHelper runs one gcx command in a new process.
// The parent re-executes the compiled test binary. This keeps process-level
// exit behavior without building a second full gcx binary during the test.
func TestAgentConformanceProcessHelper(t *testing.T) {
	if os.Getenv(agentConformanceProcessHelper) != "1" {
		return
	}

	var args []string
	if err := json.Unmarshal([]byte(os.Getenv(agentConformanceArgs)), &args); err != nil {
		t.Fatalf("decode helper arguments: %v", err)
	}

	agent.ResetForTesting()
	os.Args = append([]string{"gcx"}, args...)
	preParseAgentFlag()
	appversion.Set("test")

	cmd := root.Command("test")
	boolFlags := collectBoolFlags(cmd)
	subCmds := collectSubCmds(cmd)
	if err := root.ValidateArgs(cmd, args); err != nil {
		os.Exit(reportError(err, boolFlags, subCmds))
	}
	err := cmd.ExecuteContext(context.Background())
	os.Exit(reportError(err, boolFlags, subCmds))
}

// runGcx runs the process helper with agent mode enabled, an isolated HOME and
// XDG environment, telemetry off, and stdin closed. It returns stdout and the
// exit code; stderr is captured only to keep it out of stdout.
func runGcx(t *testing.T, args ...string) (string, int) {
	t.Helper()
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("encode helper arguments: %v", err)
	}

	home := t.TempDir()
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestAgentConformanceProcessHelper$") //nolint:gosec // trusted current test binary
	cmd.Env = []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".state"),
		// swept commands run for real: agent prune deletes from os.TempDir(),
		// so the host temp dir must not leak in
		"TMPDIR=" + home,
		"PATH=" + os.Getenv("PATH"),
		"GCX_AGENT_MODE=1",
		"GCX_TELEMETRY=off",
		"DO_NOT_TRACK=1",
		agentConformanceProcessHelper + "=1",
		agentConformanceArgs + "=" + string(encodedArgs),
	}
	cmd.Stdin = nil // exec: /dev/null — a surviving prompt reads EOF, never blocks on us

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
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

func TestAgentConformance_FailuresAreOneInBandErrorDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a gcx helper process; skipped with -short")
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

// TestAgentConformance_InvalidStackSlugIsUsageError pins the client-side slug
// validation added for issue #950: an invalid slug fails before the dry-run
// preview with a single gcx.error document and a usage exit code, in both the
// process status and the in-band payload.
func TestAgentConformance_InvalidStackSlugIsUsageError(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a gcx helper process; skipped with -short")
	}

	stdout, code := runGcx(t, "cloud", "stacks", "create",
		"--name", "t", "--slug", "my-gcx-eval", "--dry-run")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)\nstdout:\n%s", code, stdout)
	}
	doc := assertOneJSONValue(t, stdout)
	obj, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("error document is %T, want object", doc)
	}
	if obj["type"] != "gcx.error" {
		t.Fatalf("document type = %v, want gcx.error (no dry-run preview may render)", obj["type"])
	}
	if obj["schema_version"] != "1" {
		t.Fatalf("schema_version = %v, want %q", obj["schema_version"], "1")
	}
	errField, ok := obj["error"].(map[string]any)
	if !ok {
		t.Fatal("error document missing error object")
	}
	if exitCode, _ := errField["exitCode"].(float64); int(exitCode) != 2 {
		t.Fatalf("in-band exitCode = %v, want 2", errField["exitCode"])
	}
	if details, _ := errField["details"].(string); !strings.Contains(details, "lowercase") {
		t.Fatalf("details should state the slug rule, got %q", details)
	}
}

// TestAgentConformance_EveryFiniteLeafEmitsOneJSONValue sweeps EVERY leaf
// command classified finite, artifact or stream in
// testdata/output_classes.json: each is executed with no arguments in a
// fully isolated environment (no config, empty working directory, stdin
// closed, 20s timeout). Whether the command succeeds offline or fails —
// missing config, missing required args, usage error — the agent contract
// demands finite/artifact stdout hold exactly one JSON value, and stream
// stdout hold only JSON values (typed JSONL events or, pre-stream, one
// fused error document) — never prose. Any in-band exitCode (gcx.error,
// gcx.stream_end) must agree with the process exit code. This is the
// all-commands empirical check: a command that prints cobra usage text,
// prose, a second document, or a disagreeing exit code on any of these
// paths fails here by name.
func TestAgentConformance_EveryFiniteLeafEmitsOneJSONValue(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("executes every finite leaf in a helper process; skipped with -short")
	}

	raw, err := os.ReadFile("root/testdata/output_classes.json")
	if err != nil {
		t.Fatalf("reading output class fixture: %v", err)
	}
	classes := map[string]string{}
	if err := json.Unmarshal(raw, &classes); err != nil {
		t.Fatalf("parsing output class fixture: %v", err)
	}

	for _, cmdPath := range sortedKeys(classes) {
		class := classes[cmdPath]
		if class != "finite" && class != "artifact" && class != "stream" {
			continue
		}
		args := strings.Fields(cmdPath)[1:] // drop the "gcx" prefix
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()
			stdout, code, timedOut := runGcxIsolated(t, args)
			if timedOut {
				t.Fatal("command did not exit within the timeout — a prompt or editor survived agent mode")
			}
			if class == "stream" {
				assertExitCodeAgreement(t, assertOnlyJSONValues(t, stdout), code)
				return
			}
			if strings.TrimSpace(stdout) == "" {
				t.Fatalf("stdout empty — finite commands must emit exactly one JSON value even on failure paths")
			}
			doc := assertOneJSONValue(t, stdout)
			assertExitCodeAgreement(t, []any{doc}, code)
			assertSuccessfulDocumentExit(t, doc, code)
		})
	}
}

func assertSuccessfulDocumentExit(t *testing.T, doc any, code int) {
	t.Helper()
	obj, ok := doc.(map[string]any)
	if ok && obj["type"] == "gcx.error" {
		return
	}
	if code != 0 {
		t.Fatalf("successful document has exit code %d, want 0\ndocument: %v", code, doc)
	}
}

// assertOnlyJSONValues decodes stdout as a sequence of JSON values and fails
// on anything that is not JSON. Empty stdout is allowed — a stream command
// may legitimately write nothing before failing (the error document goes
// through reportError only on non-emitted paths).
func assertOnlyJSONValues(t *testing.T, stdout string) []any {
	t.Helper()
	var docs []any
	dec := json.NewDecoder(strings.NewReader(stdout))
	for {
		var v any
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			return docs
		}
		if err != nil {
			t.Fatalf("stdout holds a non-JSON value: %v\nstdout:\n%s", err, stdout)
		}
		docs = append(docs, v)
	}
}

// assertExitCodeAgreement checks every in-band exit code (gcx.error and
// gcx.stream_end documents) against the process exit code — the contract's
// "the exit code agrees with the outcome" leg, per leaf.
func assertExitCodeAgreement(t *testing.T, docs []any, code int) {
	t.Helper()
	for _, d := range docs {
		obj, ok := d.(map[string]any)
		if !ok {
			continue
		}
		if obj["type"] != "gcx.error" && obj["type"] != "gcx.stream_end" {
			continue
		}
		errObj, ok := obj["error"].(map[string]any)
		if !ok {
			continue
		}
		inBand, ok := errObj["exitCode"].(float64)
		if !ok {
			continue
		}
		if int(inBand) != code {
			t.Fatalf("in-band exitCode %v disagrees with process exit code %d\ndocument: %v", inBand, code, obj)
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// runGcxIsolated executes the binary with agent mode on, no configuration,
// an empty working directory, and a hard timeout. Returns stdout, the exit
// code, and whether the run timed out.
func runGcxIsolated(t *testing.T, args []string) (string, int, bool) {
	t.Helper()
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("encode helper arguments: %v", err)
	}
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentConformanceProcessHelper$") //nolint:gosec // trusted current test binary
	cmd.Dir = t.TempDir()                                                                        // no ./resources or other cwd pickups
	cmd.Env = []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".state"),
		// swept commands run for real: agent prune deletes from os.TempDir(),
		// so the host temp dir must not leak in
		"TMPDIR=" + home,
		"PATH=" + os.Getenv("PATH"),
		"GCX_AGENT_MODE=1",
		"GCX_TELEMETRY=off",
		"DO_NOT_TRACK=1",
		agentConformanceProcessHelper + "=1",
		agentConformanceArgs + "=" + string(encodedArgs),
	}
	cmd.Stdin = nil
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	if ctx.Err() != nil {
		return outBuf.String(), -1, true
	}
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running gcx %v: %v", args, err)
	}
	return outBuf.String(), code, false
}

func TestAgentConformance_ExplicitOverrideWins(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a gcx helper process; skipped with -short")
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
