package config

import (
	"fmt"
	"strings"
)

// CLIOptions holds CLI-level configuration options that affect command behavior
// but are not specific to any Grafana context.
type CLIOptions struct {
	// AutoApprove automatically enables the --force flag on delete operations,
	// enabling non-interactive operation in CI/CD pipelines.
	AutoApprove bool `env:"GCX_AUTO_APPROVE"`

	// DisableUpdateNotifier disables the periodic notifier that reminds users
	// when their installed gcx skills can be updated. Any non-empty value
	// disables the notifier (NO_COLOR convention).
	DisableUpdateNotifier string `env:"GCX_NO_UPDATE_NOTIFIER"`

	// Keychain overrides trusted credentials.keychain configuration. "off" is
	// the only value that disables the OS keychain and persists credentials in
	// the mode-0600 config file. "on" is the default; an unrecognized value
	// warns and resolves to "on", so a typo cannot silently write plaintext.
	Keychain string `env:"GCX_KEYCHAIN"`

	// RequireContext makes gcx refuse any invocation that would fall back to
	// current-context from the config file, so a command can only reach the
	// environment its own invocation names. Intended for workstations that
	// hold many contexts and run gcx from several sessions or coding agents at
	// once, where current-context is shared mutable state: another session's
	// `config use-context` silently retargets every later command.
	//
	// Any non-empty value except an explicit off switch enables it, so a typo
	// leaves the requirement in place rather than silently lifting it.
	RequireContext string `env:"GCX_REQUIRE_CONTEXT"`
}

// RequireContextEnabled reports whether strict context mode is on.
func (opts CLIOptions) RequireContextEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(opts.RequireContext)) {
	case "", "false", "0", "off", "no":
		return false
	default:
		return true
	}
}

// LoadCLIOptions loads CLI options from environment variables.
func LoadCLIOptions() (CLIOptions, error) {
	opts := CLIOptions{}
	if err := parseEnvTags(&opts); err != nil {
		return opts, fmt.Errorf("failed to parse CLI options: %w", err)
	}
	return opts, nil
}
