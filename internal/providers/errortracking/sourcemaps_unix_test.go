//go:build unix

package errortracking_test

import (
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertSkipsSpecialFilesInADirectory(t *testing.T) {
	e := newUpsertEnv(t)
	e.runCases(t, []upsertCase{
		{name: "a named pipe in a directory is skipped with a warning", files: []string{"dist"}, calls: sent(),
			write: map[string]string{"dist/app.js.map": appMap},
			setup: func(t *testing.T) {
				t.Helper()
				require.NoError(t, syscall.Mkfifo("dist/pipe.js.map", 0o600))
			},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asJSON(t, "app.js.map")
				assert.JSONEq(t, e.mutation(`[`+entry("app.js.map", appMap)+`]`, nativeList("skipped", "dist/pipe.js.map")), out)
			}},
		{name: "escape sequences in the names of skipped files never reach the terminal", files: []string{"dist"}, format: "text", calls: sent(),
			write: map[string]string{"dist/app.js.map": appMap, "dist/\x1b]0;owned\a\u202enotes.txt": "x"},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				assert.Contains(t, e.stderr.String(), "skipped 1 file that is not a source map: ")
				assert.Contains(t, e.stderr.String(), "]0;ownednotes.txt")
				assertNoEscapes(t, "stdout", out)
				assertNoEscapes(t, "stderr", e.stderr.String())
			}},
	})
}
