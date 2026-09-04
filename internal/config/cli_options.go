package config

import "fmt"

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
	// With keychain use on, unavailable and locked stores fail closed rather
	// than dynamically falling back during login, refresh, or ordinary writes.
	Keychain string `env:"GCX_KEYCHAIN"`
}

// LoadCLIOptions loads CLI options from environment variables.
func LoadCLIOptions() (CLIOptions, error) {
	opts := CLIOptions{}
	if err := parseEnvTags(&opts); err != nil {
		return opts, fmt.Errorf("failed to parse CLI options: %w", err)
	}
	return opts, nil
}
