package config

import (
	"os"
	"strings"
)

// keychainMode is the resolved credential-storage backend for this invocation.
type keychainMode string

const (
	keychainModeEnabled  keychainMode = "enabled"
	keychainModeDisabled keychainMode = "disabled"
)

// envKeychain is declared as CLIOptions.Keychain, which is how it reaches the
// generated environment-variable reference. It is read here directly rather
// than through LoadCLIOptions so that a malformed value in an unrelated
// variable cannot make an explicit opt-out silently resolve to enabled.
// TestKeychainEnvTagMatchesResolvedName pins the two names together.
const envKeychain = "GCX_KEYCHAIN"

// keychainModeForProcess resolves whether credentials may be stored in the OS
// keychain. It is the one place that decision is made, so GCX_KEYCHAIN is
// honoured on every path that can reach the credential store.
func keychainModeForProcess() keychainMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envKeychain))) {
	case "off":
		return keychainModeDisabled
	default:
		// One accepted value, so the setting reads the same everywhere it is
		// written down. Unset and unrecognised values both resolve to enabled:
		// with the keychain on by default, a typo in an opt-out must not
		// silently move credentials into plaintext on disk.
		return keychainModeEnabled
	}
}
