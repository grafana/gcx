package root_test

import (
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnCallEscalate_InvalidFlagValuesAreUsageErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the gcx binary; skipped with -short")
	}
	for _, tc := range []struct {
		name       string
		flags      []string
		message    string
		suggestion string
	}{
		{"blank incident ID", []string{"--title", "Page", "--incident-id="}, "--incident-id must not be blank", "Find incident IDs: gcx irm incidents list"},
		{"missing title", nil, "--title is required", "See examples: gcx irm oncall escalate --help"},
		{"blank title", []string{"--title", " \t "}, "--title is required", "See examples: gcx irm oncall escalate --help"},
	} {
		t.Run(tc.name+"/human", func(t *testing.T) {
			args := append([]string{"--agent=false", "irm", "oncall", "escalate"}, tc.flags...)
			stdout, stderr, code := runGcxStreams(t, args...)
			require.Equal(t, gcxerrors.ExitUsageError, code, "stdout: %s\nstderr: %s", stdout, stderr)
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, gcxerrors.SummaryInvalidCommandUsage)
			assert.Contains(t, stderr, tc.message)
			assert.Contains(t, stderr, tc.suggestion)
		})
		t.Run(tc.name+"/agent", func(t *testing.T) {
			args := append([]string{"irm", "oncall", "escalate"}, tc.flags...)
			stdout, code := runGcx(t, args...)
			require.Equal(t, gcxerrors.ExitUsageError, code, "%s", stdout)
			doc, ok := assertOneJSONValue(t, stdout).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "gcx.error", doc["type"])
			detail, ok := doc["error"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, gcxerrors.SummaryInvalidCommandUsage, detail["summary"])
			assert.EqualValues(t, gcxerrors.ExitUsageError, detail["exitCode"])
			assert.Contains(t, detail["details"], tc.message)
			assert.Contains(t, detail["suggestions"], tc.suggestion)
		})
	}
}
