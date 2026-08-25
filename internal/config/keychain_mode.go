package config

import "strings"

// keychainMode is the resolved credential-storage backend for this invocation.
type keychainMode string

const (
	keychainModeEnabled  keychainMode = "enabled"
	keychainModeDisabled keychainMode = "disabled"
)

// keychainModeForProcess resolves whether credentials may be stored in the OS
// keychain. It is the one place that decision is made, so GCX_KEYCHAIN is
// honoured on every path that can reach the credential store. The variable is
// declared as CLIOptions.Keychain, which is both how it is read here and how it
// reaches the generated environment-variable reference.
//
// There is deliberately no config-file equivalent: the keychain backend has to
// be chosen before a config file can be loaded, so a config-file switch could
// not be resolved from the merged config and would have to re-read and re-layer
// the files behind Load's back.
func keychainModeForProcess() keychainMode {
	opts, err := LoadCLIOptions()
	if err != nil {
		// A malformed CLI option must not be read as permission to stop using
		// the OS keychain.
		return keychainModeEnabled
	}
	return parseKeychainMode(opts.Keychain)
}

func parseKeychainMode(value string) keychainMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "disabled", "off", "false", "0":
		return keychainModeDisabled
	default:
		// Unset and unrecognised values both resolve to enabled: with the
		// keychain on by default, a typo in an opt-out must not silently move
		// credentials into plaintext on disk.
		return keychainModeEnabled
	}
}
