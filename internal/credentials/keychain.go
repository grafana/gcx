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
// reachable (unsupported platform, headless box, missing DBus), it returns a
// Store that reports ErrUnavailable on every operation. Callers must propagate
// that failure; plaintext is selected separately by trusted configuration and
// does not call Open. If the backend is reachable but locked, every operation
// reports ErrLocked instead.
func Open() Store {
	// Probe with a read for an account we never write. A working backend
	// returns ErrNotFound; an unreachable one returns a transport/platform
	// error, which means keychain-backed credential operations must fail.
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
// fatal so callers never silently downgrade them to plaintext. Errors that
// prove the backend exists but is locked become ErrLocked, which stays fatal.
func normalizeKeyringError(err error) error {
	return normalizeKeyringErrorForOS(err, runtime.GOOS)
}

func normalizeKeyringErrorForOS(err error, goos string) error {
	if err == nil || errors.Is(err, ErrUnavailable) || errors.Is(err, ErrLocked) ||
		errors.Is(err, keyring.ErrSetDataTooBig) {
		return err
	}
	// A locked backend is a reachable backend. Classify it before the
	// unavailability check so it never downgrades to a plaintext fallback.
	if nativeKeyringBackendLocked(err, goos) {
		return fmt.Errorf("%w: %w", ErrLocked, err)
	}
	if nativeKeyringBackendUnavailable(err, goos) {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return err
}

// nativeKeyringBackendLocked reports whether the error proves that a native
// keychain exists, but it is locked or cannot present the interaction needed
// to unlock it in the current session.
func nativeKeyringBackendLocked(err error, goos string) bool {
	if goos == "darwin" {
		var exitErr *exec.ExitError
		return errors.As(err, &exitErr) && darwinKeychainLockedExitCode(exitErr.ExitCode())
	}
	if !usesSecretService(goos) {
		return false
	}

	// go-keyring returns errors from godbus without a stable exported wrapper
	// at this boundary. Match the signatures that mean the collection is
	// locked, or that the unlock prompt returned no unlocked collection.
	message := strings.ToLower(err.Error())
	for _, signature := range []string{
		"org.freedesktop.secret.error.islocked",
		"failed to unlock correct collection",
	} {
		if strings.Contains(message, signature) {
			return true
		}
	}
	return false
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
	// These are the low-byte process statuses of Security framework failures
	// that prove no usable keychain exists: no default keychain, no such
	// keychain, and no available keychain.
	switch code {
	case 37, 50, 53:
		return true
	default:
		return false
	}
}

func darwinKeychainLockedExitCode(code int) bool {
	// go-keyring invokes /usr/bin/security and discards Set's stderr. Exit 154
	// is observed when a headless session reaches a locked login keychain. The
	// documented Security framework statuses 24 (dark wake: no UI possible)
	// and 36 (interaction not allowed) likewise prove that the keychain exists
	// but cannot be unlocked interactively in the current session.
	switch code {
	case 24, 36, 154:
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
