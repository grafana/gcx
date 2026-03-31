// Package agent detects whether gcx is running inside an AI agent
// environment (e.g. Claude Code, Cursor, GitHub Copilot, Amazon Q).
//
// Detection happens automatically at init() time by reading well-known
// environment variables. The result can also be influenced by the --agent
// CLI flag via [SetFlag].
package agent

import (
	"os"
	"strings"
)

// Environment variables that signal agent mode.
var agentEnvVars = []string{ //nolint:gochecknoglobals
	"GCX_AGENT_MODE",
	"CLAUDECODE",
	"CLAUDE_CODE",
	"CURSOR_AGENT",
	"GITHUB_COPILOT",
	"AMAZON_Q",
}

var (
	agentMode       bool //nolint:gochecknoglobals
	detectedFromEnv bool //nolint:gochecknoglobals
)

func init() { //nolint:gochecknoinits
	detectFromEnv()
}

// ResetForTesting re-runs environment detection from current env vars.
// Exported for use in tests only.
func ResetForTesting() {
	detectFromEnv()
}

// IsAgentMode reports whether gcx is running in agent mode.
// The value is determined by environment variables (checked at init time)
// and the --agent CLI flag (applied via [SetFlag]).
func IsAgentMode() bool {
	return agentMode
}

// DetectedFromEnv reports whether agent mode was detected from environment
// variables, as opposed to being set only via [SetFlag].
func DetectedFromEnv() bool {
	return detectedFromEnv
}

// SetFlag is called from the CLI layer after pre-parsing os.Args for the
// --agent flag. The flag is only set when the user explicitly passes
// --agent or --agent=false, so it always takes precedence over env detection.
func SetFlag(enabled bool) {
	agentMode = enabled
}

// detectFromEnv reads environment variables and sets the package-level state.
// It is called by init() and can be re-called from tests after modifying env.
func detectFromEnv() {
	detectedFromEnv = false
	agentMode = false

	// GCX_AGENT_MODE has the highest priority: an explicit falsy
	// value disables agent mode regardless of other variables.
	if v, ok := os.LookupEnv("GCX_AGENT_MODE"); ok {
		if isFalsy(v) {
			return
		}

		if isTruthy(v) {
			detectedFromEnv = true
			agentMode = true

			return
		}
	}

	// Check remaining env vars for a truthy value.
	for _, env := range agentEnvVars {
		if isTruthy(os.Getenv(env)) {
			detectedFromEnv = true
			agentMode = true

			return
		}
	}
}

// isTruthy returns true for the values "1", "true", and "yes" (case-insensitive).
func isTruthy(s string) bool {
	switch strings.ToLower(s) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// isFalsy returns true for the values "0", "false", and "no" (case-insensitive).
func isFalsy(s string) bool {
	switch strings.ToLower(s) {
	case "0", "false", "no":
		return true
	default:
		return false
	}
}
