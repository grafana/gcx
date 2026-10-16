package config

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/gofrs/flock"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/stretchr/testify/require"
)

type layeredLockTestLogger struct {
	logging.Logger

	onLocked func(string)
}

func (l *layeredLockTestLogger) WithContext(context.Context) logging.Logger { return l }
func (l *layeredLockTestLogger) With(...any) logging.Logger                 { return l }
func (l *layeredLockTestLogger) Debug(message string, args ...any) {
	if message != "Locked config for layered load" {
		return
	}
	for _, arg := range args {
		if attr, ok := arg.(slog.Attr); ok && attr.Key == "filename" {
			l.onLocked(attr.Value.String())
		}
	}
}

func TestLayeredSourceLocksReleasePartialAcquisitionOnCancel(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "a.yaml")
	secondPath := filepath.Join(root, "b.yaml")
	for _, path := range []string{firstPath, secondPath} {
		require.NoError(t, os.WriteFile(path, []byte(writeLockTestConfig), 0o600))
	}
	firstIdentity, err := canonicalConfigSourceForLayer(firstPath, "user")
	require.NoError(t, err)
	secondIdentity, err := canonicalConfigSourceForLayer(secondPath, "user")
	require.NoError(t, err)
	secondLockPath, err := configLockFile(secondIdentity)
	require.NoError(t, err)
	otherWriter := flock.New(secondLockPath)
	locked, err := otherWriter.TryLock()
	require.NoError(t, err)
	require.True(t, locked)
	t.Cleanup(func() { _ = otherWriter.Unlock() })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	acquired := make(chan struct{})
	resume := make(chan struct{})
	logger := &layeredLockTestLogger{Logger: &boundTestLogger{}, onLocked: func(identity string) {
		require.Equal(t, firstIdentity, identity, "acquisition must use canonical identity order")
		close(acquired)
		<-resume
	}}
	go func() {
		select {
		case <-acquired:
			cancel()
		case <-ctx.Done():
		}
		close(resume)
	}()
	// Reverse discovery order so this also verifies the lock order.
	_, _, err = lockLayeredSources(logging.Context(ctx, logger), []ConfigSource{
		{Path: secondPath, Type: "user"},
		{Path: firstPath, Type: "user"},
	}, "")
	require.ErrorIs(t, err, context.Canceled)

	firstLockPath, err := configLockFile(firstIdentity)
	require.NoError(t, err)
	probe := flock.New(firstLockPath)
	locked, err = probe.TryLock()
	require.NoError(t, err)
	require.True(t, locked, "cancellation must release an already acquired source lock")
	require.NoError(t, probe.Unlock())
	probe = flock.New(secondLockPath)
	locked, err = probe.TryLock()
	require.NoError(t, err)
	if locked {
		_ = probe.Unlock()
	}
	require.False(t, locked, "cancellation must not release another writer's lock")
}

func TestLayeredConflictRecoveryRunsMigrationsUnderSourceLock(t *testing.T) {
	tests := []struct {
		name       string
		contents   string
		wantBackup bool
		wantSecret bool
	}{
		{
			name: "legacy format migration",
			contents: `contexts:
  default:
    grafana:
      server: https://example.invalid
current-context: default
`,
			wantBackup: true,
		},
		{
			name: "plaintext credential migration",
			contents: `version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      token: fake-test-token
contexts:
  default:
    stack: default
current-context: default
`,
			wantSecret: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := withFakeKeychain(t)
			fixture := newLayeredMigrationFixture(t)
			t.Setenv("GCX_KEYCHAIN", "on")
			writeLayeredMigrationFixture(t, fixture.user, writeLockTestConfig)
			identity, err := canonicalConfigSourceForLayer(fixture.user, "user")
			require.NoError(t, err)
			lockPath, err := configLockFile(identity)
			require.NoError(t, err)
			probe := flock.New(lockPath)
			t.Cleanup(func() { _ = probe.Unlock() })
			loads := 0
			logger := &layeredLoadTestLogger{Logger: &boundTestLogger{}, path: fixture.user}
			logger.onLoad = func() {
				loads++
				if loads == 1 {
					writeLayeredMigrationFixture(t, fixture.user, test.contents)
					return
				}
				locked, lockErr := probe.TryLock()
				require.NoError(t, lockErr)
				if locked {
					_ = probe.Unlock()
				}
				require.False(t, locked, "recovery must hold the source writer lock during migration")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			overrides := 0
			loaded, err := LoadLayered(logging.Context(ctx, logger), "", func(cfg *Config) error {
				overrides++
				writeCtx, writeCancel := context.WithTimeout(t.Context(), 2*time.Second)
				defer writeCancel()
				return Write(writeCtx, ExplicitConfigFile(fixture.user), *cfg)
			})
			require.NoError(t, err)
			require.Equal(t, 2, loads)
			require.Equal(t, 1, overrides, "the override must run once after recovery releases its lock")
			require.False(t, loaded.migrationDeferred, "migration must not time out on its own source lock")
			raw, err := os.ReadFile(fixture.user)
			require.NoError(t, err)
			require.Contains(t, string(raw), "version: 1")
			if test.wantBackup {
				backup, backupErr := os.ReadFile(fixture.user + legacyBackupSuffix)
				require.NoError(t, backupErr)
				require.Equal(t, test.contents, string(backup))
			}
			if test.wantSecret {
				require.NotContains(t, string(raw), "fake-test-token")
				require.Contains(t, string(raw), "keychain:gcx:v2:")
				require.NotEmpty(t, store.entries)
				require.Equal(t, "fake-test-token", loaded.Stacks["default"].Grafana.APIToken)
			}
			locked, err := probe.TryLock()
			require.NoError(t, err)
			require.True(t, locked, "recovery must release its source lock")
			require.NoError(t, probe.Unlock())
		})
	}
}

func TestLayeredConflictRecoveryReusesCallerHeldLock(t *testing.T) {
	tests := []struct {
		name     string
		addLocal bool
	}{
		{name: "reuse a caller-held source"},
		{name: "reject expansion to a second source", addLocal: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withFakeKeychain(t)
			fixture := newLayeredMigrationFixture(t)
			t.Setenv("GCX_KEYCHAIN", "off")
			writeLayeredMigrationFixture(t, fixture.user, writeLockTestConfig)
			identity, err := canonicalConfigSourceForLayer(fixture.user, "user")
			require.NoError(t, err)
			lockPath, err := configLockFile(identity)
			require.NoError(t, err)
			callerLock := flock.New(lockPath)
			locked, err := callerLock.TryLock()
			require.NoError(t, err)
			require.True(t, locked)
			t.Cleanup(func() { _ = callerLock.Unlock() })

			loads := 0
			logger := &layeredLoadTestLogger{Logger: &boundTestLogger{}, path: fixture.user}
			logger.onLoad = func() {
				loads++
				if loads == 1 {
					writeLayeredMigrationFixture(t, fixture.user, "version: 1\ncontexts:\n  updated: {}\ncurrent-context: updated\n")
					if test.addLocal {
						writeLayeredMigrationFixture(t, fixture.local, writeLockTestConfig)
					}
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			loaded, err := loadLayered(logging.Context(ctx, logger), "", loadOptions{writeLockHeldFor: identity})
			if test.addLocal {
				require.ErrorContains(t, err, "config sources changed during a locked config update; retry")
				require.Equal(t, 1, loads)
				localIdentity, identityErr := canonicalConfigSourceForLayer(fixture.local, "local")
				require.NoError(t, identityErr)
				localLockPath, pathErr := configLockFile(localIdentity)
				require.NoError(t, pathErr)
				localProbe := flock.New(localLockPath)
				localLocked, probeErr := localProbe.TryLock()
				require.NoError(t, probeErr)
				require.True(t, localLocked, "recovery must not acquire a second source while reusing a caller lock")
				require.NoError(t, localProbe.Unlock())
			} else {
				require.NoError(t, err)
				require.Equal(t, 2, loads)
				require.Equal(t, "updated", loaded.CurrentContext)
			}

			probe := flock.New(lockPath)
			locked, err = probe.TryLock()
			require.NoError(t, err)
			if locked {
				_ = probe.Unlock()
			}
			require.False(t, locked, "recovery must leave the caller's lock held")
			require.NoError(t, callerLock.Unlock())
			locked, err = probe.TryLock()
			require.NoError(t, err)
			require.True(t, locked)
			require.NoError(t, probe.Unlock())
		})
	}
}

func TestLayeredConflictRecoveryInsideDiscoveredPolicyMutation(t *testing.T) {
	for _, clear := range []bool{false, true} {
		name := "set"
		if clear {
			name = "clear"
		}
		t.Run(name, func(t *testing.T) {
			withFakeKeychain(t)
			fixture := newLayeredMigrationFixture(t)
			t.Setenv("GCX_KEYCHAIN", "")
			writeLayeredMigrationFixture(t, fixture.user, "version: 1\ncredentials:\n  keychain: off\ncontexts:\n  default: {}\ncurrent-context: default\n")
			loads := 0
			logger := &layeredLoadTestLogger{Logger: &boundTestLogger{}, path: fixture.user}
			logger.onLoad = func() {
				loads++
				if loads == 1 {
					writeLayeredMigrationFixture(t, fixture.user, "version: 1\ncredentials:\n  keychain: off\ncontexts:\n  updated: {}\ncurrent-context: updated\n")
				}
			}
			on := "on"
			value := &on
			if clear {
				value = nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			source, err := MutateKeychainPolicy(logging.Context(ctx, logger), "", "", value)
			require.NoError(t, err)
			require.Equal(t, 2, loads)
			path, err := source()
			require.NoError(t, err)
			require.Equal(t, fixture.user, path)
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Contains(t, string(raw), "current-context: updated")
			if clear {
				require.NotContains(t, string(raw), "credentials:")
			} else {
				var persisted Config
				require.NoError(t, yaml.Unmarshal(raw, &persisted))
				require.NotNil(t, persisted.Credentials)
				require.Equal(t, "on", persisted.Credentials.Keychain)
			}
			identity, err := canonicalConfigSourceForLayer(path, "user")
			require.NoError(t, err)
			lockPath, err := configLockFile(identity)
			require.NoError(t, err)
			probe := flock.New(lockPath)
			locked, err := probe.TryLock()
			require.NoError(t, err)
			require.True(t, locked, "the policy transaction must release its own source lock")
			require.NoError(t, probe.Unlock())
		})
	}
}
