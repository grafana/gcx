package credentials

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	keyring "github.com/zalando/go-keyring"
)

func TestNormalizeKeyringErrorClassifiesDarwinWriteUnavailability(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the synthetic exit-status fixture requires a POSIX shell")
	}
	cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", "exit 154")
	err := cmd.Run()
	require.Error(t, err)

	got := normalizeKeyringErrorForOS(fmt.Errorf("native set: %w", err), "darwin")
	require.ErrorIs(t, got, ErrUnavailable)
	assert.Contains(t, got.Error(), "exit status 154")
}

func TestDarwinKeychainUnavailableExitCodes(t *testing.T) {
	for _, code := range []int{24, 36, 37, 50, 53, 154} {
		assert.True(t, darwinKeychainUnavailableExitCode(code), "exit code %d", code)
	}
	for _, code := range []int{1, 44, 51, 128, 255} {
		assert.False(t, darwinKeychainUnavailableExitCode(code), "exit code %d", code)
	}
}

func TestNormalizeKeyringErrorClassifiesSecretServiceUnavailability(t *testing.T) {
	tests := []error{
		errors.New("dbus: couldn't determine address of session bus"),
		errors.New("exec: \"dbus-launch\": executable file not found in $PATH"),
		errors.New("The name org.freedesktop.secrets was not provided by any .service files"),
		errors.New("org.freedesktop.DBus.Error.ServiceUnknown"),
		// A locked collection is not an unavailable backend. See
		// TestNormalizeKeyringErrorClassifiesSecretServiceLock for that case.
		errors.New("org.freedesktop.Secret.Error.NoSession: no session exists"),
		&os.PathError{Op: "dial", Path: "/run/user/1000/bus", Err: syscall.ECONNREFUSED},
	}
	for _, err := range tests {
		t.Run(err.Error(), func(t *testing.T) {
			require.ErrorIs(t, normalizeKeyringErrorForOS(err, "linux"), ErrUnavailable)
		})
	}
}

func TestNormalizeKeyringErrorClassifiesUnsupportedPlatform(t *testing.T) {
	require.ErrorIs(t, normalizeKeyringErrorForOS(keyring.ErrUnsupportedPlatform, "plan9"), ErrUnavailable)
}

func TestNormalizeKeyringErrorKeepsPermanentAndUnknownFailuresFatal(t *testing.T) {
	tests := []struct {
		name string
		err  error
		goos string
	}{
		{name: "oversized value", err: keyring.ErrSetDataTooBig, goos: "darwin"},
		{name: "generic darwin failure", err: errors.New("generic native failure"), goos: "darwin"},
		{name: "generic dbus failure", err: errors.New("org.freedesktop.Secret.Error.Protocol"), goos: "linux"},
		{name: "permission policy", err: &os.PathError{Op: "dial", Path: "/run/user/1000/bus", Err: syscall.EACCES}, goos: "linux"},
		// An unlock failure is now a locked backend, not an unknown failure. See
		// TestNormalizeKeyringErrorClassifiesSecretServiceLock for that case.
		{name: "user cancelled prompt", err: errors.New("user cancelled Secret Service prompt"), goos: "linux"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeKeyringErrorForOS(tt.err, tt.goos)
			require.ErrorIs(t, got, tt.err)
			require.NotErrorIs(t, got, ErrUnavailable)
		})
	}
}

// lockedSignatures returns the messages that prove the Secret Service exists
// but stays locked. The go-keyring library returns these as plain errors, so
// gcx must match them by text. The second entry is the message that go-keyring
// v0.2.8 returns for a locked GNOME login collection.
func lockedSignatures() map[string]error {
	return map[string]error{
		"collection is locked":                   errors.New("org.freedesktop.Secret.Error.IsLocked: the collection is locked"),
		"unlock returned no unlocked collection": errors.New("failed to unlock correct collection '/org/freedesktop/secrets/collection/login'"),
	}
}

func TestNormalizeKeyringErrorClassifiesSecretServiceLock(t *testing.T) {
	for name, lockErr := range lockedSignatures() {
		for _, goos := range []string{"linux", "freebsd", "netbsd", "openbsd", "dragonfly"} {
			t.Run(name+"/"+goos, func(t *testing.T) {
				got := normalizeKeyringErrorForOS(lockErr, goos)
				require.ErrorIs(t, got, ErrLocked)
				// A locked keychain must never become an unavailable keychain,
				// because an unavailable keychain permits a plaintext write.
				require.NotErrorIs(t, got, ErrUnavailable)
				require.NotErrorIs(t, got, ErrNotFound)
				require.ErrorIs(t, got, lockErr)
				// The user must still see the library cause after the wrap.
				assert.Contains(t, got.Error(), lockErr.Error())
			})
		}
	}
}

func TestNormalizeKeyringErrorDoesNotClassifyLockOnNonSecretServicePlatforms(t *testing.T) {
	for name, lockErr := range lockedSignatures() {
		for _, goos := range []string{"darwin", "windows"} {
			t.Run(name+"/"+goos, func(t *testing.T) {
				// These platforms do not use the Secret Service, so the same
				// text carries no meaning. The error stays unclassified.
				got := normalizeKeyringErrorForOS(lockErr, goos)
				require.ErrorIs(t, got, lockErr)
				require.NotErrorIs(t, got, ErrLocked)
				require.NotErrorIs(t, got, ErrUnavailable)
			})
		}
	}
}

func TestNormalizeKeyringErrorIsIdempotentForLockedError(t *testing.T) {
	wrapped := normalizeKeyringErrorForOS(
		errors.New("org.freedesktop.Secret.Error.IsLocked: the collection is locked"), "linux")
	require.ErrorIs(t, wrapped, ErrLocked)

	got := normalizeKeyringErrorForOS(wrapped, "linux")
	// The second call must return the same error value, not a second wrap.
	assert.Equal(t, wrapped, got)
	assert.Equal(t, 1, strings.Count(got.Error(), ErrLocked.Error()),
		"the locked prefix must appear exactly once")
}

func TestErrorStorePreservesUnexpectedProbeFailure(t *testing.T) {
	want := errors.New("unexpected keychain probe failure")
	store := errorStore{err: want}

	_, err := store.Get("account")
	require.ErrorIs(t, err, want)
	require.ErrorIs(t, store.Set("account", "secret"), want)
	require.ErrorIs(t, store.Delete("account"), want)
}

func TestErrorStorePropagatesLockedProbeFailure(t *testing.T) {
	// Open classifies the probe error, then keeps it when it is not an
	// unavailable backend. Reproduce that decision here.
	probe := errors.New("org.freedesktop.Secret.Error.IsLocked: the collection is locked")
	classified := normalizeKeyringErrorForOS(probe, "linux")
	require.NotErrorIs(t, classified, ErrUnavailable,
		"a locked probe must not select the plaintext fallback store")

	store := errorStore{err: classified}
	value, err := store.Get("account")
	assert.Empty(t, value)
	require.ErrorIs(t, err, ErrLocked)
	require.ErrorIs(t, store.Set("account", "secret"), ErrLocked)
	require.ErrorIs(t, store.Delete("account"), ErrLocked)
}

func TestUnavailableStoreStillReportsUnavailable(t *testing.T) {
	// Open selects this store only for an unreachable backend. It must never
	// report a locked backend.
	store := unavailableStore{}

	_, err := store.Get("account")
	require.ErrorIs(t, err, ErrUnavailable)
	require.NotErrorIs(t, err, ErrLocked)
	require.ErrorIs(t, store.Set("account", "secret"), ErrUnavailable)
	require.ErrorIs(t, store.Delete("account"), ErrUnavailable)
}
