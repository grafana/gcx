package testutils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SandboxConfigEnv isolates a test from every config-discovery input so that
// only config files the test writes itself can be found: HOME and the XDG
// config/state homes point at a temp dir, XDG_CONFIG_DIRS points at an empty
// temp dir (system layer), the working directory moves to an empty temp dir
// (cwd-local .gcx.yaml layer), every GRAFANA_* env var and GCX_CONFIG are
// cleared (t.Setenv registers restoration), telemetry is disabled, and
// agent-mode detection is pinned off via SetAgentMode — a plain
// t.Setenv("GCX_AGENT_MODE", ...) would be dead code because internal/agent
// caches detection at init() time.
//
// Returns the sandbox HOME. Incompatible with t.Parallel (t.Setenv/t.Chdir
// enforce this).
func SandboxConfigEnv(t *testing.T) string {
	t.Helper()
	SetAgentMode(t, false)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv("GCX_TELEMETRY", "disabled")
	t.Chdir(t.TempDir())
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GRAFANA_") || k == "GCX_CONFIG" {
			t.Setenv(k, os.Getenv(k))
			os.Unsetenv(k)
		}
	}
	return home
}
