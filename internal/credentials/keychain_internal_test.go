package credentials

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
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
		errors.New("org.freedesktop.Secret.Error.IsLocked: the collection is locked"),
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
		{name: "unlock prompt failure", err: errors.New("failed to unlock correct collection"), goos: "linux"},
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

func TestErrorStorePreservesUnexpectedProbeFailure(t *testing.T) {
	want := errors.New("unexpected keychain probe failure")
	store := errorStore{err: want}

	_, err := store.Get("account")
	require.ErrorIs(t, err, want)
	require.ErrorIs(t, store.Set("account", "secret"), want)
	require.ErrorIs(t, store.Delete("account"), want)
}
