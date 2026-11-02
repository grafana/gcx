//nolint:testpackage // Tests package-private command constructors and validation order.
package k6

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV6CommandsRejectInvalidInputBeforeConfigLoad(t *testing.T) {
	tests := []struct {
		name     string
		build    func(CloudConfigLoader) *cobra.Command
		args     []string
		stdin    string
		contains string
	}{
		{
			name:  "explicit empty idempotency key",
			build: newLoadTestsStartCommand,
			args:  []string{"1", "--idempotency-key", ""}, contains: "expected 1 to 36",
		},
		{
			name:  "negative project limit",
			build: newProjectLimitsListCommand,
			args:  []string{"--limit", "-1"}, contains: "expected 1 to 1000",
		},
		{
			name:  "invalid label key",
			build: newLabelKeysCreateCommand,
			args:  []string{"-f", "-"}, stdin: "value:\n  - key: bad key\n", contains: "invalid",
		},
		{
			name:  "project label with no key",
			build: newProjectsUpdateLabelsCommand,
			args:  []string{"1", "-f", "-", "--force"}, stdin: "value:\n  - value: smoke\n", contains: "exactly one",
		},
		{
			name:  "negative schedule list limit",
			build: newSchedulesListCommand,
			args:  []string{"--limit", "-1"}, contains: "expected 0 or more",
		},
		{
			name:  "non-positive schedule ID",
			build: newSchedulesGetCommand,
			args:  []string{"0"}, contains: "positive integer",
		},
		{
			name:  "non-positive metrics run ID",
			build: newRunsListMetricsCommand,
			args:  []string{"0"}, contains: "invalid run ID",
		},
		{
			name:  "negative schedule load test ID",
			build: newSchedulesCreateCommand,
			args:  []string{"--load-test-id", "-1", "-f", "-"}, stdin: "starts: 2026-01-01T00:00:00Z\n", contains: "positive integer",
		},
		{
			name:  "schedule request needs starts",
			build: newSchedulesCreateCommand,
			args:  []string{"--load-test-id", "1", "-f", "-"}, stdin: "recurrence_rule:\n  frequency: DAILY\n", contains: "starts is required",
		},
		{
			name:  "schedule request rejects two recurrence modes",
			build: newSchedulesCreateCommand,
			args:  []string{"--load-test-id", "1", "-f", "-"}, stdin: "starts: 2026-01-01T00:00:00Z\nrecurrence_rule:\n  frequency: DAILY\ncron:\n  schedule: '@daily'\n  time_zone: UTC\n",
			contains: "only one",
		},
		{
			name:  "schedule update validates input before config",
			build: newSchedulesUpdateCommand,
			args:  []string{"1", "-f", "-"}, stdin: "starts: not-a-time\n", contains: "expected RFC3339",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := test.build(nil)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetIn(strings.NewReader(test.stdin))
			cmd.SetArgs(test.args)
			err := cmd.Execute()
			require.Error(t, err)
			require.ErrorContains(t, err, test.contains)
			assert.NotContains(t, stdout.String(), "stack")
		})
	}
}
