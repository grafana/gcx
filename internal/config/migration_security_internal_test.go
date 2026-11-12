package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func writePrivateMigrationFixture(t *testing.T, path string, contents []byte) {
	t.Helper()
	file, err := createPrivateLegacyBackup(path)
	require.NoError(t, err)
	_, writeErr := file.Write(contents)
	closeErr := file.Close()
	require.NoError(t, writeErr)
	require.NoError(t, closeErr)
}
