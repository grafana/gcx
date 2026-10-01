package host

import (
	"context"
	"time"

	"github.com/fsnotify/fsnotify"
	keyring "github.com/zalando/go-keyring"
	"k8s.io/client-go/discovery"
	diskcached "k8s.io/client-go/discovery/cached/disk"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
)

// Third-party libraries that touch the host on their own.

// Keyring is a handle on the OS credential store (github.com/zalando/go-keyring).
// Obtain one with [OpenKeyring].
type Keyring struct{}

// OpenKeyring returns a handle on the OS credential store. Inside a sandbox
// the store belongs to the embedding process, so it is unavailable.
func OpenKeyring(ctx context.Context) (Keyring, error) {
	if Sandboxed(ctx) {
		return Keyring{}, opErr("keyring")
	}
	return Keyring{}, nil
}

// Get mirrors [keyring.Get].
func (Keyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

// Set mirrors [keyring.Set].
func (Keyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

// Delete mirrors [keyring.Delete].
func (Keyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

// NewWatcher mirrors [fsnotify.NewWatcher].
func NewWatcher(ctx context.Context) (*fsnotify.Watcher, error) {
	if Sandboxed(ctx) {
		return nil, opErr("watch filesystem")
	}
	return fsnotify.NewWatcher()
}

// NewCachedDiscoveryClientForConfig mirrors
// [diskcached.NewCachedDiscoveryClientForConfig]. Inside a sandbox there is no
// disk to cache on, so discovery results are cached in memory for the
// lifetime of the returned client instead.
func NewCachedDiscoveryClientForConfig(ctx context.Context, config *rest.Config, discoveryCacheDir, httpCacheDir string, ttl time.Duration) (discovery.CachedDiscoveryInterface, error) {
	if Sandboxed(ctx) {
		client, err := discovery.NewDiscoveryClientForConfig(config)
		if err != nil {
			return nil, err
		}
		return memory.NewMemCacheClient(client), nil
	}
	return diskcached.NewCachedDiscoveryClientForConfig(config, discoveryCacheDir, httpCacheDir, ttl)
}
