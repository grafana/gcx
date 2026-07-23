package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

const (
	configCheckProcessHelper       = "GCX_CONFIG_CHECK_PROCESS_HELPER"
	configSetFallbackProcessHelper = "GCX_CONFIG_SET_FALLBACK_PROCESS_HELPER"
)

func TestConfigSetPlaintextFallbackWarningProcess(t *testing.T) {
	const token = "synthetic-plaintext-fallback-token"
	const warning = "Warning: credential store unavailable; credentials remain in plaintext on disk; install or unlock your OS credential store (Keychain, Credential Manager, or Secret Service) to enable encrypted credential storage"

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
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestConfigSetPlaintextFallbackProcessHelper$") //nolint:gosec
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			cmd.Env = append(os.Environ(),
				configSetFallbackProcessHelper+"=1",
				"GCX_CONFIG_SET_FALLBACK_PATH="+configPath,
				"GCX_CONFIG_SET_FALLBACK_TOKEN="+token,
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

			if err := cmd.Run(); err != nil {
				t.Fatalf("config set failed: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
			}
			// The agent output contract makes config set emit one JSON
			// mutation document; the human default stays silent.
			if agentMode == "true" {
				var doc map[string]any
				if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
					t.Fatalf("agent stdout is not one JSON document: %v; stdout=%q", err, stdout.String())
				}
				if doc["type"] != "gcx.config.mutation" {
					t.Fatalf("agent stdout document type = %v, want gcx.config.mutation", doc["type"])
				}
			} else if stdout.Len() != 0 {
				t.Fatalf("config set wrote unexpected stdout: %q", stdout.String())
			}
			if got := bytes.Count(stderr.Bytes(), []byte(warning)); got != 1 {
				t.Fatalf("plaintext fallback warning count = %d, want 1; stderr=%q", got, stderr.String())
			}
			if bytes.Contains(stdout.Bytes(), []byte(token)) || bytes.Contains(stderr.Bytes(), []byte(token)) {
				t.Fatalf("plaintext token appeared in command output; stdout=%q stderr=%q", stdout.String(), stderr.String())
			}

			raw, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(raw, []byte(token)) || bytes.Contains(raw, []byte("keychain:gcx:v2:")) {
				t.Fatalf("expected deliberate plaintext fallback without a sentinel: %q", raw)
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

func TestConfigSetPlaintextFallbackProcessHelper(_ *testing.T) {
	if os.Getenv(configSetFallbackProcessHelper) != "1" {
		return
	}

	agent.ResetForTesting()
	os.Args = []string{
		"gcx", "config", "set",
		"--config", os.Getenv("GCX_CONFIG_SET_FALLBACK_PATH"),
		"stacks.smoke.grafana.token", os.Getenv("GCX_CONFIG_SET_FALLBACK_TOKEN"),
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
