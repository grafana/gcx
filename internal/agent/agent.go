// Package agent detects whether gcx is running inside an AI agent
// environment. It uses known identity signals and explicit agent names.
//
// Detection happens automatically at init() time by reading well-known
// environment variables. The result can also be influenced by the --agent
// CLI flag via [SetFlag].
package agent

import (
	"os"
	"strings"
)

// Keep these inputs in sync with the Env tags used by the docs generator.
const (
	envMode          = "GCX_AGENT_MODE"
	envName          = "GCX_AGENT_NAME"
	envAIIdentity    = "AI_AGENT"
	envGooseIdentity = "AGENT"
)

// Env documents the explicit agent controls for the environment reference.
// Detection reads these variables directly, before command construction.
type Env struct {
	// Mode enables agent mode with 1, true, or yes. The values 0, false, and
	// no disable it. The --agent flag takes precedence.
	Mode string `env:"GCX_AGENT_MODE"`
	// Name identifies the calling agent and enables agent mode unless disabled
	// explicitly. Use a supported name from the agent detection reference.
	// Unknown names are ignored and are never sent in usage telemetry.
	Name string `env:"GCX_AGENT_NAME"`
	// AIIdentity follows the AI_AGENT convention. A supported name or
	// name@version enables agent mode. Unknown names and versions are not sent.
	AIIdentity string `env:"AI_AGENT"`
	// GooseIdentity detects Goose when AGENT is goose. Other values are ignored.
	GooseIdentity string `env:"AGENT"`
}

// Boolean signals accept only 1, true, or yes. Keep fork-specific signals
// before their parent signals: Kilo also sets OPENCODE, and Qwen forks Gemini.
var harnessEnvVars = []struct{ envVar, name string }{ //nolint:gochecknoglobals
	{"CLAUDECODE", "claude-code"},
	{"CLAUDE_CODE", "claude-code"},
	{"CURSOR_AGENT", "cursor"},
	{"GITHUB_COPILOT", "github-copilot"},
	{"COPILOT_CLI", "github-copilot"},
	{"AMAZON_Q", "amazon-q"},
	{"KILO", "kilo-code"},
	{"QWEN_CODE", "qwen-code"},
	{"GEMINI_CLI", "gemini-cli"},
	{"CODEX_SHELL", "codex"},
	{"CLINE_ACTIVE", "cline"},
	{"GOOSE_TERMINAL", "goose"},
	{"OPENCODE", "opencode"},
	{"PI_CODING_AGENT", "pi"},
}

// Session markers contain an identifier, not a boolean. Read only presence;
// never return or export their values. Do not use config paths or API keys.
var harnessSessionVars = []struct{ envVar, name string }{ //nolint:gochecknoglobals
	{"CODEX_THREAD_ID", "codex"},
	{"CODEX_SESSION_ID", "codex"},
	{"COPILOT_AGENT_SESSION_ID", "github-copilot"},
}

// EnvironmentVariables returns the detection inputs. Test helpers use this
// list to clear inherited signals before they test human and agent behavior.
func EnvironmentVariables() []string {
	vars := make([]string, 0, 4+len(harnessEnvVars)+len(harnessSessionVars))
	vars = append(vars, envMode, envName, envAIIdentity, envGooseIdentity)
	for _, h := range harnessEnvVars {
		vars = append(vars, h.envVar)
	}
	for _, h := range harnessSessionVars {
		vars = append(vars, h.envVar)
	}
	return vars
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
	if v, ok := os.LookupEnv(envMode); ok {
		if isFalsy(v) {
			return
		}

		if isTruthy(v) {
			detectedFromEnv = true
			agentMode = true

			return
		}
	}

	if Name() != "" {
		detectedFromEnv = true
		agentMode = true
	}
}

// Name returns a fixed agent name, or "" when no supported identity is set.
// It still reports the name when GCX_AGENT_MODE or --agent disables agent mode.
// The gcx-specific override comes first. Native signals precede shared identity
// fallbacks because a shared variable can be inherited from an outer harness.
func Name() string {
	if name := supportedName(os.Getenv(envName)); name != "" {
		return name
	}
	for _, h := range harnessEnvVars {
		if isTruthy(os.Getenv(h.envVar)) {
			return h.name
		}
	}
	for _, h := range harnessSessionVars {
		if os.Getenv(h.envVar) != "" {
			return h.name
		}
	}
	if name := supportedName(os.Getenv(envAIIdentity)); name != "" {
		return name
	}
	// Only Goose's use of the bare AGENT variable has upstream evidence.
	if strings.EqualFold(strings.TrimSpace(os.Getenv(envGooseIdentity)), "goose") {
		return "goose"
	}
	return ""
}

// supportedName restricts telemetry to known labels. Shared identity variables
// can carry name@version; the version is discarded. No raw value is returned.
func supportedName(value string) string {
	name, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), "@")
	switch name {
	case "claude-code", "codex", "cursor", "github-copilot", "gemini-cli",
		"opencode", "cline", "kilo-code", "kiro", "factory-droid", "amp",
		"augment", "junie", "devin", "openhands", "goose", "aider",
		"qwen-code", "pi", "crush", "amazon-q":
		return name
	case "github-copilot-cli":
		return "github-copilot"
	case "kilo", "kilocode":
		return "kilo-code"
	case "droid":
		return "factory-droid"
	case "auggie":
		return "augment"
	default:
		return ""
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
