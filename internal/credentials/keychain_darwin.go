package credentials

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

const (
	darwinSecurityBinary = "/usr/bin/security"
	darwinProbeTimeout   = 5 * time.Second
)

// headlessKeychainError reports ErrUnavailable when macOS has no default
// keychain for the current HOME. Without one, a write through security(1)
// raises a blocking "Keychain Not Found" GUI dialog, and the migration retries
// on every invocation. show-keychain-info only reads the default keychain's
// settings, never prompts, and exits 37 when no default keychain exists.
func headlessKeychainError() error {
	return darwinDefaultKeychainError(darwinSecurityBinary, darwinProbeTimeout)
}

func darwinDefaultKeychainError(binary string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := exec.CommandContext(ctx, binary, "show-keychain-info").Run()
	if err == nil {
		return nil
	}
	// Only a proven-missing keychain is fatal here. Any other probe failure
	// (timeout, odd exit status) is left to the normal Open probe and Set path.
	if normalized := normalizeKeyringErrorForOS(err, "darwin"); errors.Is(normalized, ErrUnavailable) {
		return normalized
	}
	return nil
}
