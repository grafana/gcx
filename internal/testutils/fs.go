package testutils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// CreateTempFile creates a temporary file with the given content.
// The file will be automatically removed when the test that created it is
// complete.
func CreateTempFile(t *testing.T, content string) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "gcx_tests_")
	require.NoError(t, err)

	_, err = file.WriteString(content)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = os.Remove(file.Name())
	})

	return file.Name()
}
