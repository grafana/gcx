package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/stretchr/testify/require"
)

type layeredIdentityTestLogger struct {
	logging.Logger

	onLock func()
}

func (l *layeredIdentityTestLogger) WithContext(context.Context) logging.Logger { return l }
func (l *layeredIdentityTestLogger) With(...any) logging.Logger                 { return l }
func (l *layeredIdentityTestLogger) Debug(message string, _ ...any) {
	if message == "Locked config for layered load" {
		l.onLock()
	}
}

func TestLayeredLocksReuseCanonicalIdentity(t *testing.T) {
	fixture := newLayeredMigrationFixture(t)
	writeLayeredMigrationFixture(t, fixture.user, "version: 1\ncontexts: {}\n")
	require.NoError(t, os.MkdirAll(filepath.Dir(fixture.system), 0o700))
	require.NoError(t, os.Symlink(fixture.user, fixture.system))
	sources, err := DiscoverSources()
	require.NoError(t, err)
	require.Len(t, sources, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	identities, release, err := lockLayeredSources(ctx, sources, "")
	require.NoError(t, err)
	defer release()
	require.Equal(t, identities[fixture.user], identities[fixture.system])
	lockPath, err := configLockFile(identities[fixture.user])
	require.NoError(t, err)
	probe := flock.New(lockPath)
	locked, err := probe.TryLock()
	require.NoError(t, err)
	require.False(t, locked)
	release()
	locked, err = probe.TryLock()
	require.NoError(t, err)
	require.True(t, locked)
	require.NoError(t, probe.Unlock())
}

func TestLayeredLockedLoadRejectsDiscoveryChanges(t *testing.T) {
	for _, kind := range []string{"new layer", "symlink target"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newLayeredMigrationFixture(t)
			writeLayeredMigrationFixture(t, fixture.user, "version: 1\ncontexts: {}\n")
			logger := &layeredIdentityTestLogger{Logger: &boundTestLogger{}}
			logger.onLock = func() {
				if kind == "new layer" {
					writeLayeredMigrationFixture(t, fixture.local, "version: 1\ncontexts: {}\n")
					return
				}
				replacement := filepath.Join(t.TempDir(), "config.yaml")
				writeLayeredMigrationFixture(t, replacement, "version: 1\ncontexts: {}\n")
				require.NoError(t, os.Remove(fixture.user))
				require.NoError(t, os.Symlink(replacement, fixture.user))
			}
			_, err := loadLayeredSourcesLocked(logging.Context(t.Context(), logger), loadOptions{})
			require.ErrorContains(t, err, "changed while acquiring layered load locks")
		})
	}
}

func TestLayeredLocksCanceledWithCallerLock(t *testing.T) {
	fixture := newLayeredMigrationFixture(t)
	writeLayeredMigrationFixture(t, fixture.user, "version: 1\ncontexts: {}\n")
	identity, err := canonicalConfigSource(fixture.user)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = lockLayeredSources(ctx, []ConfigSource{{Path: fixture.user, Type: "user"}}, identity)
	require.ErrorIs(t, err, context.Canceled)
	_, err = loadLayeredSourcesLocked(ctx, loadOptions{writeLockHeldFor: identity})
	require.ErrorIs(t, err, context.Canceled)
}

func TestLayeredLockedLoadMigratesAliasedSources(t *testing.T) {
	withFakeKeychain(t)
	fixture := newLayeredMigrationFixture(t)
	t.Setenv("GCX_KEYCHAIN", "on")
	writeLayeredMigrationFixture(t, fixture.user, "version: 1\nstacks:\n  default:\n    grafana:\n      server: https://example.invalid\n      token: fake-test-token\ncontexts:\n  default:\n    stack: default\ncurrent-context: default\n")
	require.NoError(t, os.MkdirAll(filepath.Dir(fixture.system), 0o700))
	require.NoError(t, os.Symlink(fixture.user, fixture.system))
	_, err := loadLayeredSourcesLocked(t.Context(), loadOptions{})
	require.NoError(t, err)
}
