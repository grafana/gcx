package root_test

import (
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnCallEscalate_BlankIncidentIDIsUsageError(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the gcx binary; skipped with -short")
	}
	for _, tc := range []struct {
		name, agentFlag string
	}{
		{"human", "--agent=false"},
		{"agent", "--agent=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runGcxStreams(t, tc.agentFlag, "irm", "oncall", "escalate", "--title", "Page", "--incident-id=")
			require.Equal(t, gcxerrors.ExitUsageError, code, "stdout: %s\nstderr: %s", stdout, stderr)
			if tc.name == "human" {
				assert.Empty(t, stdout)
				assert.Contains(t, stderr, gcxerrors.SummaryInvalidCommandUsage)
				assert.Contains(t, stderr, "--incident-id must not be blank")
				return
			}
			doc, ok := assertOneJSONValue(t, stdout).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "gcx.error", doc["type"])
			detail, ok := doc["error"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, gcxerrors.SummaryInvalidCommandUsage, detail["summary"])
			assert.EqualValues(t, gcxerrors.ExitUsageError, detail["exitCode"])
			assert.Contains(t, detail["details"], "--incident-id must not be blank")
		})
	}
}
