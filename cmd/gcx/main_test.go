package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
)

// TestReportError_EmittedError pins the atomic-stdout-ownership contract:
// a command that already wrote its complete result document returns an
// EmittedError, and reportError must exit with the carried code without
// writing a second document (the function returns before any output path).
func TestReportError_EmittedError(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	cases := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "partial failure code carried through wrapping",
			err:  fmt.Errorf("push: %w", gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, errors.New("2 failed"))),
			want: gcxerrors.ExitPartialFailure,
		},
		{
			name: "general error code",
			err:  gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, nil),
			want: gcxerrors.ExitGeneralError,
		},
		{
			name: "nil error still exits zero",
			err:  nil,
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reportError(tc.err, nil, nil)
			if got != tc.want {
				t.Fatalf("reportError() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestIsSilentCancellation pins which errors take the quiet exit-5 route.
// An interrupted invocation prints nothing, but an EmittedError has already
// written its own result document and owns its exit code, even when its cause
// chain reaches context.Canceled.
func TestIsSilentCancellation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "bare context.Canceled", err: context.Canceled, want: true},
		{name: "wrapped context.Canceled", err: fmt.Errorf("query: %w", context.Canceled), want: true},
		{name: "deadline exceeded is not a cancellation", err: context.DeadlineExceeded, want: false},
		{name: "unrelated error", err: errors.New("boom"), want: false},
		{
			name: "EmittedError wrapping context.Canceled keeps its own exit code",
			err:  gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, context.Canceled),
			want: false,
		},
		{
			name: "EmittedError carrying exit 5 still reports through reportError",
			err:  gcxerrors.NewEmittedError(gcxerrors.ExitCancelled, context.Canceled),
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if got := isSilentCancellation(ctx, tc.err); got != tc.want {
				t.Fatalf("isSilentCancellation() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A signal context carries a cause describing the signal (Go 1.26), and
// net/http surfaces that cause instead of context.Canceled. Before Go 1.26.5
// the cause did not report itself as context.Canceled, so an interrupted
// request has to be recognised through the context's own cause — but only when
// the error really carries it, never for an unrelated failure that happened to
// land while the context was done.
func TestIsSilentCancellationMatchesSignalCause(t *testing.T) {
	signalCause := errors.New("interrupt signal received")

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "request failure carrying the signal cause",
			err:  &url.Error{Op: "Get", URL: "http://example.invalid", Err: signalCause},
			want: true,
		},
		{name: "the bare cause", err: signalCause, want: true},
		{name: "unrelated failure during a canceled context", err: errors.New("boom"), want: false},
		{
			name: "EmittedError carrying the signal cause keeps its own exit code",
			err:  gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, signalCause),
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(signalCause)
			if got := isSilentCancellation(ctx, tc.err); got != tc.want {
				t.Fatalf("isSilentCancellation() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A cause is only consulted once the context is actually canceled: an ordinary
// failure in a live invocation must never be read as a cancellation.
func TestIsSilentCancellationIgnoresLiveContext(t *testing.T) {
	if isSilentCancellation(context.Background(), errors.New("boom")) {
		t.Fatal("an error in a live context must not be treated as a cancellation")
	}
}

// An EmittedError carrying exit 5 keeps that code through reportError, so the
// usage event classifies it as canceled like any other exit-5 invocation.
func TestReportErrorEmittedCancellationKeepsExitFive(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	err := fmt.Errorf("push: %w", gcxerrors.NewEmittedError(gcxerrors.ExitCancelled, context.Canceled))
	if got := reportError(err, nil, nil); got != gcxerrors.ExitCancelled {
		t.Fatalf("reportError() = %d, want %d", got, gcxerrors.ExitCancelled)
	}
}

// TestAbandonsExport pins the full matrix that decides whether exitWith may
// disarm the signal handler. The process-level tests cover the two diagonal
// cases against a real binary; this covers the other two, which no command in
// the tree can reach without a second subprocess harness.
func TestAbandonsExport(t *testing.T) {
	cases := []struct {
		name        string
		interrupted bool
		exitCode    int
		want        bool
	}{
		{
			name:        "interrupted and canceled",
			interrupted: true, exitCode: gcxerrors.ExitCancelled, want: true,
		},
		{
			name:        "interrupted but successful",
			interrupted: true, exitCode: gcxerrors.ExitSuccess, want: false,
		},
		{
			name:        "interrupted but failed",
			interrupted: true, exitCode: gcxerrors.ExitGeneralError, want: false,
		},
		{
			name:        "canceled without an interrupt",
			interrupted: false, exitCode: gcxerrors.ExitCancelled, want: false,
		},
		{
			name:        "neither",
			interrupted: false, exitCode: gcxerrors.ExitSuccess, want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := abandonsExport(tc.interrupted, tc.exitCode); got != tc.want {
				t.Fatalf("abandonsExport(%t, %d) = %t, want %t",
					tc.interrupted, tc.exitCode, got, tc.want)
			}
		})
	}
}

const (
	configCheckProcessHelper                  = "GCX_CONFIG_CHECK_PROCESS_HELPER"
	configSetUnavailableKeychainProcessHelper = "GCX_CONFIG_SET_UNAVAILABLE_KEYCHAIN_PROCESS_HELPER"
)

func TestConfigSetUnavailableKeychainFailsClosedProcess(t *testing.T) {
	const token = "synthetic-unavailable-keychain-token"

	for _, agentMode := range []string{"false", "true"} {
		t.Run("agent-mode="+agentMode, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			contents := []byte(`version: 1
stacks:
  smoke:
    grafana:
      server: https://example.invalid
      auth-method: token
contexts:
  smoke:
    stack: smoke
current-context: smoke
`)
			if err := os.WriteFile(configPath, contents, 0o600); err != nil {
				t.Fatal(err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestConfigSetUnavailableKeychainProcessHelper$") //nolint:gosec
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			cmd.Env = append(os.Environ(),
				configSetUnavailableKeychainProcessHelper+"=1",
				"GCX_CONFIG_SET_UNAVAILABLE_KEYCHAIN_PATH="+configPath,
				"GCX_CONFIG_SET_UNAVAILABLE_KEYCHAIN_TOKEN="+token,
				"GCX_AGENT_MODE="+agentMode,
				"GCX_TELEMETRY=disabled",
				"GCX_NO_UPDATE_NOTIFIER=1",
				"NO_COLOR=1",
				"HOME="+t.TempDir(),
				"XDG_CONFIG_HOME="+t.TempDir(),
				"XDG_CONFIG_DIRS="+t.TempDir(),
				"XDG_CACHE_HOME="+t.TempDir(),
				"XDG_STATE_HOME="+t.TempDir(),
				"GCX_CONFIG=",
				"GRAFANA_SERVER=",
				"GRAFANA_USER=",
				"GRAFANA_PASSWORD=",
				"GRAFANA_TOKEN=",
				"GRAFANA_PROXY_ENDPOINT=",
				"GRAFANA_ORG_ID=",
				"GRAFANA_STACK_ID=",
			)

			if err := cmd.Run(); err == nil {
				t.Fatalf("config set unexpectedly succeeded; stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			// The typed error envelope must be emitted in agent mode; the human
			// diagnostic belongs on stderr without corrupting stdout.
			if agentMode == "true" {
				var doc map[string]any
				if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
					t.Fatalf("agent stdout is not one JSON error document: %v; stdout=%q", err, stdout.String())
				}
				if doc["type"] != "gcx.error" {
					t.Fatalf("agent stdout document type = %v, want gcx.error", doc["type"])
				}
				if stderr.Len() != 0 {
					t.Fatalf("agent error wrote unexpected stderr: %q", stderr.String())
				}
			} else if stdout.Len() != 0 {
				t.Fatalf("config set wrote unexpected stdout: %q", stdout.String())
			}
			if agentMode == "false" && !bytes.Contains(stderr.Bytes(), []byte("Keychain unavailable")) {
				t.Fatalf("human output did not name the unavailable keychain: %q", stderr.String())
			}
			if bytes.Contains(stdout.Bytes(), []byte(token)) || bytes.Contains(stderr.Bytes(), []byte(token)) {
				t.Fatalf("plaintext token appeared in command output; stdout=%q stderr=%q", stdout.String(), stderr.String())
			}

			raw, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte(token)) || bytes.Contains(raw, []byte("keychain:gcx:v2:")) {
				t.Fatalf("unavailable keychain wrote a credential unexpectedly: %q", raw)
			}
			info, err := os.Stat(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("config permissions = %o, want 600", got)
			}
		})
	}
}

func TestConfigSetUnavailableKeychainProcessHelper(_ *testing.T) {
	if os.Getenv(configSetUnavailableKeychainProcessHelper) != "1" {
		return
	}

	agent.ResetForTesting()
	os.Args = []string{
		"gcx", "config", "set",
		"--config", os.Getenv("GCX_CONFIG_SET_UNAVAILABLE_KEYCHAIN_PATH"),
		"stacks.smoke.grafana.token", os.Getenv("GCX_CONFIG_SET_UNAVAILABLE_KEYCHAIN_TOKEN"),
	}
	preParseAgentFlag()
	cmd := root.Command("test")
	err := cmd.ExecuteContext(context.Background())
	os.Exit(reportError(err, collectBoolFlags(cmd), collectSubCmds(cmd)))
}

func TestConfigCheckProcessExit(t *testing.T) {
	invalidConfigPath := filepath.Join(t.TempDir(), "invalid-config.yaml")
	contents := []byte("version: 1\ncontexts:\n  broken: {}\ncurrent-context: broken\n")
	if err := os.WriteFile(invalidConfigPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	versionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/health":
			_, _ = w.Write([]byte(`{"version":"11.6.0"}`))
		case "/api":
			_, _ = w.Write([]byte(`{"kind":"APIVersions","apiVersion":"v1","versions":[]}`))
		case "/apis":
			_, _ = w.Write([]byte(`{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(versionServer.Close)
	versionConfigPath := filepath.Join(t.TempDir(), "version-config.yaml")
	versionConfig := fmt.Sprintf(`version: 1
stacks:
  old:
    grafana:
      server: %q
      org-id: 1
      auth-method: token
contexts:
  old:
    stack: old
current-context: old
`, versionServer.URL)
	if err := os.WriteFile(versionConfigPath, []byte(versionConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name          string
		agentMode     string
		configPath    string
		grafanaServer string
		grafanaToken  string
		wantExitCode  int
		wantOutput    string
	}{
		{name: "human invalid config", agentMode: "false", configPath: invalidConfigPath, wantExitCode: 1, wantOutput: "context references no stack"},
		{name: "agent invalid config", agentMode: "true", configPath: invalidConfigPath, wantExitCode: 1, wantOutput: "context references no stack"},
		{name: "human incompatible version", agentMode: "false", configPath: versionConfigPath, grafanaServer: versionServer.URL, grafanaToken: "test-token", wantExitCode: 6, wantOutput: "gcx requires Grafana 12.0.0 or later"},
		{name: "agent incompatible version", agentMode: "true", configPath: versionConfigPath, grafanaServer: versionServer.URL, grafanaToken: "test-token", wantExitCode: 6, wantOutput: "gcx requires Grafana 12.0.0 or later"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			// Re-exec the trusted current test binary to verify the actual process exit path.
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestConfigCheckProcessHelper$") //nolint:gosec
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			cmd.Env = append(os.Environ(),
				configCheckProcessHelper+"=1",
				"GCX_CONFIG_CHECK_PATH="+tc.configPath,
				"GCX_AGENT_MODE="+tc.agentMode,
				"GCX_TELEMETRY=disabled",
				"NO_COLOR=1",
				"HOME="+t.TempDir(),
				"XDG_CONFIG_HOME="+t.TempDir(),
				"XDG_CONFIG_DIRS="+t.TempDir(),
				"XDG_CACHE_HOME="+t.TempDir(),
				"XDG_STATE_HOME="+t.TempDir(),
				"GRAFANA_SERVER="+tc.grafanaServer,
				"GRAFANA_USER=",
				"GRAFANA_PASSWORD=",
				"GRAFANA_TOKEN="+tc.grafanaToken,
				"GRAFANA_PROXY_ENDPOINT=",
				"GRAFANA_ORG_ID=",
				"GRAFANA_STACK_ID=",
			)

			err := cmd.Run()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected process failure, got %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
			}
			if exitErr.ExitCode() != tc.wantExitCode {
				t.Fatalf("exit code = %d, want %d; stdout=%q stderr=%q", exitErr.ExitCode(), tc.wantExitCode, stdout.String(), stderr.String())
			}
			assertConfigCheckProcessOutput(t, tc.agentMode == "true", tc.wantOutput, stdout.Bytes(), stderr.Bytes())
		})
	}
}

func assertConfigCheckProcessOutput(t *testing.T, agentMode bool, wantOutput string, stdout, stderr []byte) {
	t.Helper()
	if agentMode {
		if !json.Valid(stdout) || !bytes.Contains(stdout, []byte(`"error":`)) {
			t.Fatalf("agent stdout is not one in-band JSON error document: %q", stdout)
		}
		if !bytes.Contains(stdout, []byte(wantOutput)) {
			t.Fatalf("agent error details missing %q: %q", wantOutput, stdout)
		}
		if !bytes.Contains(stderr, []byte("Configuration:")) || !bytes.Contains(stderr, []byte("Connectivity:")) {
			t.Fatalf("complete diagnostic report missing from agent stderr: %q", stderr)
		}
		return
	}

	if !bytes.Contains(stdout, []byte("Configuration:")) || !bytes.Contains(stdout, []byte("Connectivity:")) {
		t.Fatalf("complete diagnostic report missing from stdout: %q", stdout)
	}
	if !bytes.Contains(stdout, []byte(wantOutput)) {
		t.Fatalf("diagnostic output missing %q: %q", wantOutput, stdout)
	}
	if len(stderr) != 0 {
		t.Fatalf("secondary human error written to stderr: %q", stderr)
	}
	if bytes.Contains(stdout, []byte(`"error":`)) {
		t.Fatalf("unexpected JSON error appended to human output: %q", stdout)
	}
}

func TestConfigCheckProcessHelper(_ *testing.T) {
	if os.Getenv(configCheckProcessHelper) != "1" {
		return
	}

	agent.ResetForTesting()
	os.Args = []string{"gcx", "config", "check", "--config", os.Getenv("GCX_CONFIG_CHECK_PATH")}
	preParseAgentFlag()
	cmd := root.Command("test")
	err := cmd.ExecuteContext(context.Background())
	os.Exit(reportError(err, collectBoolFlags(cmd), collectSubCmds(cmd)))
}

func TestParsePseudoVersion(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		wantCommit string
		wantDate   string
	}{
		{
			name:       "valid pseudo-version",
			version:    "v0.1.1-0.20260401105553-2fbda4a2dd27",
			wantCommit: "2fbda4a",
			wantDate:   "2026-04-01T10:55:53Z",
		},
		{
			name:       "pseudo-version with +dirty suffix",
			version:    "v0.1.1-0.20260401105553-2fbda4a2dd27+dirty",
			wantCommit: "2fbda4a",
			wantDate:   "2026-04-01T10:55:53Z",
		},
		{
			name:       "pseudo-version with +incompatible suffix",
			version:    "v2.0.1-0.20260401105553-2fbda4a2dd27+incompatible",
			wantCommit: "2fbda4a",
			wantDate:   "2026-04-01T10:55:53Z",
		},
		{
			name:       "tagged version",
			version:    "v1.0.0",
			wantCommit: "",
			wantDate:   "",
		},
		{
			name:       "pre-release tagged version",
			version:    "v1.0.0-rc.1",
			wantCommit: "",
			wantDate:   "",
		},
		{
			name:       "devel",
			version:    "(devel)",
			wantCommit: "",
			wantDate:   "",
		},
		{
			name:       "empty string",
			version:    "",
			wantCommit: "",
			wantDate:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCommit, gotDate := parsePseudoVersion(tt.version)
			if gotCommit != tt.wantCommit {
				t.Errorf("commit = %q, want %q", gotCommit, tt.wantCommit)
			}
			if gotDate != tt.wantDate {
				t.Errorf("date = %q, want %q", gotDate, tt.wantDate)
			}
		})
	}
}
