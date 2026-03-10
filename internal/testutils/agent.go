package testutils

import (
	"os"

	"github.com/grafana/grafanactl/internal/agent"
)

func init() { //nolint:gochecknoinits
	// Clear agent-mode env vars so that tests are not affected by the
	// host environment (e.g. CLAUDECODE=1 inside Claude Code sessions).
	// Without this, agent.init() caches the host state and BindFlags
	// defaults to JSON output, breaking tests that expect YAML/text.
	for _, env := range []string{
		"GRAFANACTL_AGENT_MODE",
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
