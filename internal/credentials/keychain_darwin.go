package credentials

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	darwinSecurityBinary = "/usr/bin/security"
	darwinProbeTimeout   = 5 * time.Second
)

// headlessKeychainError reports ErrUnavailable when macOS has no default
// keychain for the current HOME. Without one, a write through security(1)
// raises a blocking "Keychain Not Found" GUI dialog, and the migration retries
// on every invocation.
//
// The probe is `security default-keychain`, which only reads the keychain
// search-list preference. It does not open the keychain, so a locked login
// keychain still exits 0 and the probe cannot raise an unlock prompt. When
// there is no default keychain it exits 1 with "A default keychain could not
// be found." on stderr.
func headlessKeychainError() error {
	return darwinDefaultKeychainError(darwinSecurityBinary, darwinProbeTimeout)
}

// darwinNoDefaultKeychainMessage is what security(1) prints, with exit status
// 1, when the keychain search list has no default keychain.
const darwinNoDefaultKeychainMessage = "default keychain could not be found"

func darwinDefaultKeychainError(binary string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "default-keychain").CombinedOutput()
	if err == nil {
		return nil
	}
	// Only a proven-missing keychain is fatal here. Any other probe failure
	// (timeout, odd exit status) is left to the normal Open probe and Set path.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 &&
		strings.Contains(strings.ToLower(string(out)), darwinNoDefaultKeychainMessage) {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if normalized := normalizeKeyringErrorForOS(err, "darwin"); errors.Is(normalized, ErrUnavailable) {
		return normalized
	}
	return nil
}
