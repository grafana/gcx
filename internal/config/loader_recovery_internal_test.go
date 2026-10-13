package config

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/stretchr/testify/require"
)

// The loader logs after preflight and before it consumes the frozen source.
// Use that point to schedule a write without a timing-dependent goroutine.
type layeredLoadTestLogger struct {
	logging.Logger

	path   string
	onLoad func()
}

func (l *layeredLoadTestLogger) WithContext(context.Context) logging.Logger {
	return l
}

func (l *layeredLoadTestLogger) With(...any) logging.Logger {
	return l
}

func (l *layeredLoadTestLogger) Debug(message string, args ...any) {
	if message != "Loading config" {
		return
	}
	for _, arg := range args {
		if attr, ok := arg.(slog.Attr); ok && attr.Key == "filename" && attr.Value.String() == l.path {
			l.onLoad()
		}
	}
}

func TestLoadLayeredRecoversRevisionConflicts(t *testing.T) {
	tests := []struct {
		name           string
		changes        int
		addLocal       bool
		cancel         bool
		invalidVersion bool
		overrideError  bool
		wantAttempts   int
		wantOverrides  int
		wantContext    string
	}{
		{name: "stable source", wantAttempts: 1, wantOverrides: 1, wantContext: "a"},
		{name: "one update loads fresh bytes and policy", changes: 1, wantAttempts: 2, wantOverrides: 1, wantContext: "b"},
		{name: "retry discovers a new layer", changes: 1, addLocal: true, wantAttempts: 2, wantOverrides: 1, wantContext: "local"},

		{name: "external updates still fail under lock", changes: 2, wantAttempts: 2},
		{name: "cancellation stops retry", changes: 1, cancel: true, wantAttempts: 1},
		{name: "retry rejects an unsupported version", changes: 1, invalidVersion: true, wantAttempts: 1},
		{name: "ordinary override error does not retry", overrideError: true, wantAttempts: 1, wantOverrides: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withFakeKeychain(t)
			fixture := newLayeredMigrationFixture(t)
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv(envKeychain, "")
			cfg := Config{
				Version:        ConfigVersion,
				CurrentContext: "a",
				Credentials:    &CredentialsConfig{Keychain: "off"},
				Contexts:       map[string]*Context{"a": {}, "b": {}},
				keychainPolicy: keychainPolicy{mode: keychainModeDisabled, source: "test fixture"},
			}
			source := ExplicitConfigFile(fixture.user)
			require.NoError(t, os.MkdirAll(filepath.Dir(fixture.user), 0o700))
			require.NoError(t, Write(t.Context(), source, cfg))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			attempts := 0
			logger := &layeredLoadTestLogger{Logger: &boundTestLogger{}, path: fixture.user}
			logger.onLoad = func() {
				attempts++
				if attempts > test.changes {
					return
				}
				if cfg.CurrentContext == "a" {
					cfg.CurrentContext = "b"
				} else {
					cfg.CurrentContext = "a"
				}
				cfg.Credentials = &CredentialsConfig{Keychain: "on"}
				if attempts == 1 {
					require.NoError(t, Write(t.Context(), source, cfg))
				} else {
					// An external editor does not take the gcx writer lock.
					writeLayeredMigrationFixture(t, fixture.user, "version: 1\ncontexts:\n  external: {}\ncurrent-context: external\n")
				}
				if test.addLocal {
					writeLayeredMigrationFixture(t, fixture.local, "version: 1\ncontexts:\n  local: {}\ncurrent-context: local\n")
				}
				if test.invalidVersion {
					writeLayeredMigrationFixture(t, fixture.user, "version: 999\ncontexts: {}\n")
				}
				if test.cancel {
					cancel()
				}
			}
			overrides := 0
			ordinaryError := errors.New("config example.yaml changed while loading layered configuration; retry")
			loaded, err := LoadLayered(logging.Context(ctx, logger), "", func(*Config) error {
				overrides++
				if test.overrideError {
					return ordinaryError
				}
				return nil
			})
			require.Equal(t, test.wantAttempts, attempts)
			require.Equal(t, test.wantOverrides, overrides)
			switch {
			case test.cancel:
				require.ErrorIs(t, err, context.Canceled)
			case test.invalidVersion:
				var unsupported UnsupportedVersionError
				require.ErrorAs(t, err, &unsupported)
			case test.overrideError:
				require.ErrorIs(t, err, ordinaryError)
			case test.changes == 2:
				var changed *layeredConfigChangedError
				require.ErrorAs(t, err, &changed)
				require.Equal(t, fixture.user, changed.path)
				require.EqualError(t, err, "config "+fixture.user+" changed while loading layered configuration; retry")
			default:
				require.NoError(t, err)
				require.Equal(t, test.wantContext, loaded.CurrentContext)
				if test.changes > 0 {
					require.Equal(t, keychainModeEnabled, loaded.keychainPolicy.mode)
				}
				if test.addLocal {
					require.Len(t, loaded.Sources, 2)
				}
			}
		})
	}
}

func TestLoadLayeredCanceledBeforeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := LoadLayered(ctx, "missing-config.yaml")
	require.ErrorIs(t, err, context.Canceled)
}
