package testutils

import (
	"os"
	"testing"

	"github.com/grafana/gcx/internal/agent"
)

func init() { //nolint:gochecknoinits
	// Clear agent-mode env vars so that tests are not affected by the
	// host environment (e.g. CLAUDECODE=1 inside Claude Code sessions).
	// Without this, agent.init() caches the host state and BindFlags
	// defaults to JSON output, breaking tests that expect YAML/text.
	for _, env := range []string{
		"GCX_AGENT_MODE",
		"CLAUDECODE",
		"CLAUDE_CODE",
		"CURSOR_AGENT",
		"GITHUB_COPILOT",
		"AMAZON_Q",
	} {
		os.Unsetenv(env)
	}

	agent.ResetForTesting()
}

// SetAgentMode pins agent-mode detection for the duration of a test, so TTY
// and agent-mode output shapes can be asserted deterministically even when
// the test runs inside an agent harness (e.g. CLAUDECODE=1). The cleanup is
// registered before t.Setenv so it runs after the env restore (LIFO),
// re-detecting from the original environment. Incompatible with t.Parallel
// (t.Setenv enforces this).
func SetAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	t.Cleanup(agent.ResetForTesting)
	if enabled {
		t.Setenv("GCX_AGENT_MODE", "true")
	} else {
		t.Setenv("GCX_AGENT_MODE", "false")
	}
	agent.ResetForTesting()
}

// PinArgv fixes os.Args for the duration of a test so behavior derived from
// the real invocation argv (e.g. list truncation continuation commands) is
// deterministic under `go test`. os.Args is process-global and the swap is
// unsynchronized — do not combine with t.Parallel.
func PinArgv(t *testing.T, argv ...string) {
	t.Helper()
	old := os.Args
	os.Args = argv
	t.Cleanup(func() { os.Args = old })
}
