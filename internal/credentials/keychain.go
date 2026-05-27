package credentials

import (
	"errors"
	"runtime"
	"sync"

	"github.com/99designs/keyring"
)

// keychainStore wraps a 99designs/keyring opened against the OS-native backend.
type keychainStore struct {
	ring keyring.Keyring
}

// Open returns a Store backed by the OS keychain. If the keychain cannot be
// opened (headless box, missing DBus, locked session), it returns a Store that
// reports ErrUnavailable on every operation so callers can fall back to
// plaintext.
func Open() Store {
	ring, err := keyring.Open(keyring.Config{
		ServiceName:     service,
		AllowedBackends: nativeBackends(),

		// macOS: reuse the gcx keychain item across processes.
		KeychainTrustApplication: true,

		// Linux Secret Service collection — "login" is the default unlocked
		// collection on GNOME Keyring / KWallet.
		LibSecretCollectionName: "login",
	})
	if err != nil {
		return unavailableStore{}
	}
	return &keychainStore{ring: ring}
}

// nativeBackends returns the keyring backends gcx will use for the current OS.
// We intentionally exclude FileBackend — when the OS keychain is unavailable
// we prefer plaintext-with-warning over a passphrase-prompted encrypted file.
func nativeBackends() []keyring.BackendType {
	switch runtime.GOOS {
	case "darwin":
		return []keyring.BackendType{keyring.KeychainBackend}
	case "windows":
		return []keyring.BackendType{keyring.WinCredBackend}
	case "linux":
		return []keyring.BackendType{
			keyring.SecretServiceBackend,
			keyring.KWalletBackend,
		}
	default:
		return nil
	}
}

func (s *keychainStore) Get(key string) (string, error) {
	item, err := s.ring.Get(key)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return string(item.Data), nil
}

func (s *keychainStore) Set(key, value string) error {
	return s.ring.Set(keyring.Item{
		Key:   key,
		Data:  []byte(value),
		Label: "gcx: " + key,
	})
}

func (s *keychainStore) Delete(key string) error {
	err := s.ring.Remove(key)
	if errors.Is(err, keyring.ErrKeyNotFound) {
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
