//go:build !windows

package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func makeLegacyConfigWritableByOthers(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.Chmod(path, 0o660))
}

func makeMigrationDirectoryReadOnly(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.Chmod(path, 0o500))
	t.Cleanup(func() { _ = os.Chmod(path, 0o700) })
}
