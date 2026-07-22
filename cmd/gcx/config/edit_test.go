package config //nolint:testpackage // White-box tests exercise unexported createConfigForType.

import (
	"os"
	"path/filepath"
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
