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

	// Keychain turns the OS keychain off when set to "off", leaving credentials
	// in plaintext in the config file. That is the only option on a machine
	// whose keychain is permanently unavailable. "off" is the only value that
	// disables it; every other value, recognised or not, leaves the keychain in
	// use, so a typo cannot silently write credentials in plaintext.
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
