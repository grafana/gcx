package extensions_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/extensions"
)

// writeScript drops an executable shell script and returns an extensions.Installed
// pointing at it. Skipped on Windows, which has no /bin/sh.
func writeScript(t *testing.T, body string) *extensions.Installed {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixtures are not portable to windows")
	}
	path := filepath.Join(t.TempDir(), "entry")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	return &extensions.Installed{Name: "demo", Entrypoint: path}
}

func TestRunPassesEnvironmentAndArgs(t *testing.T) {
	ext := writeScript(t, `echo "bin=$GCX_EXT_GCX_BIN ctx=$GCX_EXT_CONTEXT agent=$GCX_EXT_AGENT_MODE name=$GCX_EXT_NAME args=$*"`)

	var stdout bytes.Buffer
	err := ext.Run(context.Background(), extensions.RunOptions{
		Args:      []string{"provision", "--dry-run"},
		Stdout:    &stdout,
		Stderr:    &bytes.Buffer{},
		GCXBin:    "/usr/local/bin/gcx",
		Context:   "prod",
		AgentMode: true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	got := strings.TrimSpace(stdout.String())
	want := "bin=/usr/local/bin/gcx ctx=prod agent=true name=demo args=provision --dry-run"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRunPropagatesExitCode(t *testing.T) {
	ext := writeScript(t, "exit 4\n")

	err := ext.Run(context.Background(), extensions.RunOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})

	var exitErr *extensions.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an extensions.ExitError, got %v", err)
	}
	if exitErr.Code != 4 {
		t.Fatalf("expected exit code 4, got %d", exitErr.Code)
	}
}

func TestRunReportsMissingEntrypoint(t *testing.T) {
	ext := &extensions.Installed{Name: "demo", Entrypoint: filepath.Join(t.TempDir(), "gone")}

	err := ext.Run(context.Background(), extensions.RunOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "reinstall") {
		t.Fatalf("expected a reinstall hint, got %v", err)
	}
}
