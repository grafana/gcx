package config //nolint:testpackage // White-box tests exercise unexported createConfigForType.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	internalConfig "github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateConfigForTypeLocalDoesNotFollowExistingSymlink(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	target := filepath.Join(t.TempDir(), "victim.yaml")
	original := []byte("victim: unchanged\n")
	require.NoError(t, os.WriteFile(target, original, 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(work, internalConfig.LocalConfigFileName)))

	path, err := createConfigForType("local")
	require.ErrorContains(t, err, "symlinks are not allowed")
	assert.Empty(t, path)
	after, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, original, after)
}

func TestCreateConfigForTypeLocalCreatesWithoutReplacingExistingFile(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	path := filepath.Join(work, internalConfig.LocalConfigFileName)
	original := []byte("version: 1\ncontexts:\n  existing: {}\ncurrent-context: existing\n")
	require.NoError(t, os.WriteFile(path, original, 0o600))

	createdPath, err := createConfigForType("local")
	require.NoError(t, err)
	assert.Equal(t, path, createdPath)
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, original, after)
}

func TestResolveRawEditTargetExplicitDoesNotParseConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: 999\nnot-valid-for-this-build: true\n"), 0o600))

	target, err := resolveRawEditTarget(path, nil, false)
	require.NoError(t, err)
	assert.Equal(t, path, target)
}

func TestResolveRawEditTargetRepairsAmbiguousCloudDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ambiguous.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
version: 1
stacks:
  prod:
    grafana:
      server: https://mystack.grafana.net
  dev:
    grafana:
      server: https://mystack.grafana-dev.net
cloud:
  shared:
    token: cloud-token
contexts:
  prod:
    stack: prod
    cloud: shared
  dev:
    stack: dev
    cloud: shared
current-context: prod
`), 0o600))

	_, err := internalConfig.LoadLayered(t.Context(), path)
	require.ErrorContains(t, err, "referenced by contexts in different Cloud environments")
	target, err := resolveRawEditTarget(path, nil, false)
	require.NoError(t, err)
	assert.Equal(t, path, target)
}

func TestResolveRawEditTargetHonorsGCXConfigWithoutParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.yaml")
	require.NoError(t, os.WriteFile(path, []byte("contexts: [not-valid-yaml\n"), 0o600))
	t.Setenv(internalConfig.ConfigFileEnvVar, path)

	target, err := resolveRawEditTarget("", nil, false)
	require.NoError(t, err)
	assert.Equal(t, path, target)
}

func TestResolveRawEditTargetSelectsBrokenDiscoveredLayer(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CONFIG_DIRS", filepath.Join(home, "system-config"))
	t.Setenv(internalConfig.ConfigFileEnvVar, "")
	t.Chdir(work)

	userPath := filepath.Join(home, ".config", internalConfig.StandardConfigFolder, internalConfig.StandardConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(userPath), 0o700))
	require.NoError(t, os.WriteFile(userPath, []byte("version: 999\n"), 0o600))
	localPath := filepath.Join(work, internalConfig.LocalConfigFileName)
	require.NoError(t, os.WriteFile(localPath, []byte("cloud: [semantically-broken\n"), 0o600))

	target, err := resolveRawEditTarget("", []string{"user"}, false)
	require.NoError(t, err)
	assert.Equal(t, userPath, target)
	target, err = resolveRawEditTarget("", []string{"local"}, false)
	require.NoError(t, err)
	assert.Equal(t, localPath, target)

	// A named layer remains a usable repair escape hatch even when the shell
	// normally bypasses layering through GCX_CONFIG.
	explicitPath := filepath.Join(t.TempDir(), "explicit.yaml")
	require.NoError(t, os.WriteFile(explicitPath, []byte("version: 999\n"), 0o600))
	t.Setenv(internalConfig.ConfigFileEnvVar, explicitPath)
	target, err = resolveRawEditTarget("", []string{"user"}, false)
	require.NoError(t, err)
	assert.Equal(t, userPath, target)
	t.Setenv(internalConfig.ConfigFileEnvVar, "")

	_, err = resolveRawEditTarget("", nil, false)
	require.ErrorContains(t, err, "multiple config files loaded")
}

func TestResolveRawEditTargetRejectsConflictingExplicitAndLayerSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: 1\n"), 0o600))

	_, err := resolveRawEditTarget(path, []string{"user"}, false)
	require.ErrorContains(t, err, "cannot combine --config with a config layer")
}

func TestEditCommandOpensUnsupportedExplicitConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test editor fixture is a POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), "future.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: 999\n"), 0o600))
	marker := filepath.Join(t.TempDir(), "opened-path")
	editor := filepath.Join(t.TempDir(), "editor")
	require.NoError(t, os.WriteFile(editor, []byte("#!/bin/sh\nprintf '%s' \"$1\" > \"$GCX_EDIT_TEST_OUTPUT\"\n"), 0o600))
	require.NoError(t, os.Chmod(editor, 0o700))
	t.Setenv("EDITOR", editor)
	t.Setenv("GCX_EDIT_TEST_OUTPUT", marker)

	cmd := editCmd(&Options{ConfigFile: path})
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	opened, err := os.ReadFile(marker)
	require.NoError(t, err)
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	assert.Equal(t, abs, string(opened))
}
