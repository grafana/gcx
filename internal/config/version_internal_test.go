package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRejectsUnsupportedVersionBeforeSideEffects(t *testing.T) {
	store := withFakeKeychain(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte(`
version: 999
stacks:
  prod:
    grafana:
      server: https://prod.example
      token: should-not-reach-keychain
contexts:
  prod:
    stack: prod
current-context: prod
`)
	require.NoError(t, os.WriteFile(path, original, 0o600))

	_, err := Load(context.Background(), ExplicitConfigFile(path))
	var versionErr UnsupportedVersionError
	require.ErrorAs(t, err, &versionErr)
	assert.Equal(t, int64(999), versionErr.Version)
	assert.Empty(t, store.entries)
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, original, got)
	_, statErr := os.Stat(path + legacyBackupSuffix)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestLoadRejectsMalformedDeclaredVersionBeforeLegacyMigration(t *testing.T) {
	store := withFakeKeychain(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte(`
version: "future"
contexts:
  prod:
    grafana:
      server: https://prod.example
      token: should-not-reach-keychain
current-context: prod
`)
	require.NoError(t, os.WriteFile(path, original, 0o600))

	_, err := Load(context.Background(), ExplicitConfigFile(path))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config version")
	assert.Empty(t, store.entries)
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, original, got)
	_, statErr := os.Stat(path + legacyBackupSuffix)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestWriteRejectsUnsupportedVersionWithoutChangingFile(t *testing.T) {
	withFakeKeychain(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("version: 1\ncontexts: {}\n")
	require.NoError(t, os.WriteFile(path, original, 0o600))

	err := Write(context.Background(), ExplicitConfigFile(path), Config{Version: 999})
	var versionErr UnsupportedVersionError
	require.ErrorAs(t, err, &versionErr)
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, original, got)
}

func TestReadDiagnosticsRejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
version: 999
contexts: {}
current-context: ""
diagnostics:
  telemetry: enabled
`), 0o600))

	diagnostics, err := readDiagnostics(path)
	var versionErr UnsupportedVersionError
	require.ErrorAs(t, err, &versionErr)
	assert.Nil(t, diagnostics)
}
