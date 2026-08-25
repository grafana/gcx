package config

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/output"
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
	mode, rejected := parseKeychainEnv(os.Getenv(envKeychain))
	if rejected != "" {
		warnUnrecognisedKeychainValue(rejected)
	}
	return mode
}

// parseKeychainEnv returns the resolved mode, plus the value to warn about when
// it was neither empty nor the single accepted one.
func parseKeychainEnv(value string) (keychainMode, string) {
	trimmed := strings.TrimSpace(value)
	switch strings.ToLower(trimmed) {
	case "":
		return keychainModeEnabled, ""
	case "off":
		return keychainModeDisabled, ""
	default:
		// One accepted value, so the setting reads the same everywhere it is
		// written down, and an unrecognised one keeps the keychain in use: a
		// typo in an opt-out must not move credentials into plaintext on disk.
		// Failing that way silently would leave someone who wrote
		// GCX_KEYCHAIN=disabled wondering why the keychain is still in use, so
		// the caller warns.
		return keychainModeEnabled, trimmed
	}
}

func unrecognisedKeychainWarning(value string) string {
	return fmt.Sprintf("%s=%q is not a recognized value and was ignored; the OS credential store is still in use. Set %s=off to disable it.",
		envKeychain, value, envKeychain)
}

// warnUnrecognisedKeychainValueOnce keeps the notice to one per process. The
// store is resolved on both the load and the write path, so without the latch a
// single command could repeat it.
//
//nolint:gochecknoglobals // process-wide latch for a once-per-invocation notice.
var warnUnrecognisedKeychainValueOnce sync.Once

func warnUnrecognisedKeychainValue(value string) {
	warnUnrecognisedKeychainValueOnce.Do(func() {
		output.EmitWarn(os.Stderr, unrecognisedKeychainWarning(value))
	})
}
