package credentials

import (
	"errors"
	"sync"

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
		return unavailableStore{}
	}
	return keychainStore{}
}

func (keychainStore) Get(key string) (string, error) {
	value, err := keyring.Get(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

func (keychainStore) Set(key, value string) error {
	return keyring.Set(service, key, value)
}

func (keychainStore) Delete(key string) error {
	err := keyring.Delete(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// unavailableStore is returned by Open when no working backend was found.
// Every operation returns ErrUnavailable so callers fall back to plaintext.
type unavailableStore struct{}

func (unavailableStore) Get(string) (string, error) { return "", ErrUnavailable }
func (unavailableStore) Set(string, string) error   { return ErrUnavailable }
func (unavailableStore) Delete(string) error        { return ErrUnavailable }

//nolint:gochecknoglobals // process-wide latch; see WarnUnavailableOnce.
var warnOnce sync.Once

// WarnUnavailableOnce emits the supplied warning at most once per process.
func WarnUnavailableOnce(emit func()) {
	warnOnce.Do(emit)
}
