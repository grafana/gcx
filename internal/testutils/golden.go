package testutils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Golden asserts that got matches testdata/<name>.golden, pinning rendered
// output byte for byte. Set GCX_UPDATE_GOLDEN=1 to rewrite the files.
func Golden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")

	if os.Getenv("GCX_UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))

		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file — rerun with GCX_UPDATE_GOLDEN=1")

	assert.Equal(t, string(want), got)
}
