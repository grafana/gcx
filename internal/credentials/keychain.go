package credentials

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"

	keyring "github.com/zalando/go-keyring"
)

// probeAccount is a never-stored account name used by Open to detect whether a
// working keychain backend is reachable.
const probeAccount = "__gcx_probe__"

// keychainStore is a Store backed by the OS-native keychain via
// github.com/zalando/go-keyring: macOS Keychain (/usr/bin/security), Windows
// Credential Manager, and the Linux/BSD Secret Service DBus interface (GNOME
// Keyring, or KWallet when it exposes org.freedesktop.secrets).
type keychainStore struct{}

// Open returns a Store backed by the OS keychain. If no working backend is
// reachable (unsupported platform, headless box, missing DBus, locked
// session), it returns a Store that reports ErrUnavailable on every operation
// so callers can fall back to plaintext.
func Open() Store {
	// Probe with a read for an account we never write. A working backend
	// returns ErrNotFound; an unreachable one returns a transport/platform
	// error, which means we should fall back to plaintext.
	if _, err := keyring.Get(service, probeAccount); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		err = normalizeKeyringError(err)
		if errors.Is(err, ErrUnavailable) {
			return unavailableStore{}
		}
		// Open cannot return an error. Preserve an unexpected probe failure in a
		// store that fails every later operation instead of silently treating a
		// permanent or programming error as permission to write plaintext.
		return errorStore{err: err}
	}
	return keychainStore{}
}

func (keychainStore) Get(key string) (string, error) {
	value, err := keyring.Get(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", normalizeKeyringError(err)
	}
	return value, nil
}

func (keychainStore) Set(key, value string) error {
	return normalizeKeyringError(keyring.Set(service, key, value))
}

func (keychainStore) Delete(key string) error {
	err := keyring.Delete(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return normalizeKeyringError(err)
}

// normalizeKeyringError converts only errors that prove the native credential
// backend is unreachable in the current session into ErrUnavailable. In
// particular, value-size, input, permission-policy, and unknown errors remain
// fatal so callers never silently downgrade them to plaintext.
func normalizeKeyringError(err error) error {
	return normalizeKeyringErrorForOS(err, runtime.GOOS)
}

func normalizeKeyringErrorForOS(err error, goos string) error {
	if err == nil || errors.Is(err, ErrUnavailable) || errors.Is(err, keyring.ErrSetDataTooBig) {
		return err
	}
	if nativeKeyringBackendUnavailable(err, goos) {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return err
}

func nativeKeyringBackendUnavailable(err error, goos string) bool {
	if errors.Is(err, keyring.ErrUnsupportedPlatform) ||
		errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOTCONN) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}

	if goos == "darwin" {
		var exitErr *exec.ExitError
		return errors.As(err, &exitErr) && darwinKeychainUnavailableExitCode(exitErr.ExitCode())
	}
	if !usesSecretService(goos) {
		return false
	}

	// go-keyring returns errors from godbus without a stable exported wrapper
	// at this boundary. Match the specific transport/service signatures that
	// mean no usable Secret Service exists; do not accept generic DBus errors.
	message := strings.ToLower(err.Error())
	for _, signature := range []string{
		"dbus: couldn't determine address of session bus",
		"dbus: connection closed",
		"cannot autolaunch d-bus",
		"unable to autolaunch a dbus-daemon",
		"dbus-launch",
		"org.freedesktop.dbus.error.serviceunknown",
		"org.freedesktop.dbus.error.namehasnoowner",
		"org.freedesktop.dbus.error.noserver",
		"org.freedesktop.dbus.error.disconnected",
		"org.freedesktop.dbus.error.noreply",
		"org.freedesktop.secret.error.islocked",
		"org.freedesktop.secret.error.nosession",
		"the name org.freedesktop.secrets was not provided",
		"object does not exist at path",
	} {
		if strings.Contains(message, signature) {
			return true
		}
	}
	return false
}

func usesSecretService(goos string) bool {
	switch goos {
	case "dragonfly", "freebsd", "linux", "netbsd", "openbsd":
		return true
	default:
		return false
	}
}

func darwinKeychainUnavailableExitCode(code int) bool {
	// go-keyring invokes /usr/bin/security and discards Set's stderr. The
	// observed 154 status is emitted by a headless/locked macOS session. The
	// other values are the low-byte process statuses of documented Security
	// framework failures: dark wake, interaction disallowed, no default
	// keychain, no such keychain, and no available keychain. Authentication
	// failure (51) and user cancellation (128) are intentionally excluded.
	switch code {
	case 24, 36, 37, 50, 53, 154:
		return true
	default:
		return false
	}
}

// unavailableStore is returned by Open when no working backend was found.
// Every operation returns ErrUnavailable so callers fall back to plaintext.
type unavailableStore struct{}

func (unavailableStore) Get(string) (string, error) { return "", ErrUnavailable }
func (unavailableStore) Set(string, string) error   { return ErrUnavailable }
func (unavailableStore) Delete(string) error        { return ErrUnavailable }

// errorStore retains an unexpected Open probe error. It prevents an unknown
// backend failure from being mistaken for an unavailable backend and silently
// downgrading a credential to plaintext.
type errorStore struct{ err error }

func (s errorStore) Get(string) (string, error) { return "", s.err }
func (s errorStore) Set(string, string) error   { return s.err }
func (s errorStore) Delete(string) error        { return s.err }

//nolint:gochecknoglobals // process-wide latch; see WarnUnavailableOnce.
var warnOnce sync.Once

// WarnUnavailableOnce emits the supplied warning at most once per process.
func WarnUnavailableOnce(emit func()) {
	warnOnce.Do(emit)
}
