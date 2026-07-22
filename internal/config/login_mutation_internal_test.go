package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGuardedLoginLoadRejectsChangedPlaintextWithoutKeychainWrites(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("version: 1\ncontexts:\n  prod: {}\ncurrent-context: prod\n")
	require.NoError(t, os.WriteFile(path, original, 0o600))

	before, err := Load(t.Context(), ExplicitConfigFile(path))
	require.NoError(t, err)
	guard := before.NewLoginMutationGuard("prod", LoginMutationUnified)
	changed := []byte(`version: 1
stacks:
  prod:
    grafana:
      server: https://attacker.invalid
      token: attacker-plaintext
contexts:
  prod:
    stack: prod
current-context: prod
`)
	require.NoError(t, os.WriteFile(path, changed, 0o600))
	store.setCalls = 0
	store.deleteCalls = 0

	_, err = LoadLoginMutationGuarded(t.Context(), ExplicitConfigFile(path), guard)
	require.ErrorContains(t, err, "Configuration changed during authentication")
	assert.Zero(t, store.setCalls)
	assert.Zero(t, store.deleteCalls)
	raw, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, changed, raw)
}

func TestGuardedLoginLoadRejectsNonTargetPlaintextWithoutKeychainWrites(t *testing.T) {
	store := newBoundTestStore()
	useBoundTestStore(t, store)
	home := t.TempDir()
	userDir := filepath.Join(home, ".config")
	workDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", userDir)
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv("GCX_CONFIG", "")
	t.Chdir(workDir)

	userPath := filepath.Join(userDir, StandardConfigFolder, StandardConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(userPath), 0o755))
	require.NoError(t, os.WriteFile(userPath, []byte("version: 1\ncontexts:\n  prod: {}\ncurrent-context: prod\n"), 0o600))
	localPath := filepath.Join(workDir, LocalConfigFileName)
	require.NoError(t, os.WriteFile(localPath, []byte("version: 1\ncontexts:\n  other: {}\n"), 0o600))

	effective, err := LoadLayered(t.Context(), "")
	require.NoError(t, err)
	var userSource ConfigSource
	for _, source := range effective.Sources {
		if source.Type == "user" {
			userSource = source
		}
	}
	require.NotEmpty(t, userSource.Path)
	mutationCtx := ContextWithConfigSource(t.Context(), userSource)
	persisted, err := Load(mutationCtx, ExplicitConfigFile(userPath))
	require.NoError(t, err)
	guard, err := persisted.NewLoginMutationGuard("prod", LoginMutationUnified).WithDiscoverySnapshot(&effective)
	require.NoError(t, err)
	changedLocal := []byte(`version: 1
cloud:
  attacker:
    token: attacker-plaintext
    oauth-url: https://attacker.invalid
    api-url: https://attacker.invalid
contexts:
  prod:
    cloud: attacker
`)
	require.NoError(t, os.WriteFile(localPath, changedLocal, 0o600))
	store.setCalls = 0
	store.deleteCalls = 0

	_, err = LoadLoginMutationGuarded(mutationCtx, ExplicitConfigFile(userPath), guard)
	require.ErrorContains(t, err, "Configuration changed during authentication")
	assert.Zero(t, store.setCalls)
	assert.Zero(t, store.deleteCalls)
	raw, readErr := os.ReadFile(localPath)
	require.NoError(t, readErr)
	assert.Equal(t, changedLocal, raw)
}
