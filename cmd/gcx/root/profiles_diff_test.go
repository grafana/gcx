package root_test

import (
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Both mounts share DiffCmd. Exercise each error producer once through the
// real root/converter/process; leaf tests cover the individual input rules.
func TestProfileDiff_UsageErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the gcx binary; skipped with -short")
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"validation", []string{"profiles", "diff", "{}", "{}", "--profile-type", "cpu", "--max-nodes", "0"}, "--max-nodes"},
		{"request bounds", []string{"datasources", "pyroscope", "diff", "{}", "{}", "--profile-type", "cpu"}, "--before-from"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, code := runGcx(t, tc.args...)
			require.Equal(t, gcxerrors.ExitUsageError, code, "%s", stdout)
			doc, ok := assertOneJSONValue(t, stdout).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "gcx.error", doc["type"])
			detail, ok := doc["error"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, gcxerrors.SummaryInvalidCommandUsage, detail["summary"])
			assert.EqualValues(t, gcxerrors.ExitUsageError, detail["exitCode"])
			assert.Contains(t, detail["details"], tc.want)
		})
	}
}
