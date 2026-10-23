package alert_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupsStatusTableCodec_Encode(t *testing.T) {
	codec := alert.GroupsStatusTable().Codec("table")
	assert.Equal(t, "table", string(codec.Format()))

	groups := []alert.RuleGroup{
		{
			Name:           "group-1",
			LastEvaluation: "2024-01-15T10:00:00Z",
			Rules: []alert.RuleStatus{
				{State: alert.StateFiring},
				{State: alert.StateFiring},
				{State: alert.StatePending},
				{State: alert.StateInactive},
				{State: alert.StateInactive},
			},
		},
		{
			Name:           "group-2",
			LastEvaluation: "0001-01-01T00:00:00Z",
			Rules:          []alert.RuleStatus{},
		},
	}

	var buf bytes.Buffer
	err := codec.Encode(&buf, groups)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "GROUP")
	assert.Contains(t, output, "FIRING")
	assert.Contains(t, output, "PENDING")
	assert.Contains(t, output, "INACTIVE")
	assert.Contains(t, output, "LAST_EVAL")
	assert.Contains(t, output, "group-1")
	assert.Contains(t, output, "2024-01-15T10:00:00Z")
	assert.Contains(t, output, "never", "zero time should display as 'never'")

	lines := strings.Split(strings.TrimSpace(output), "\n")
	require.Len(t, lines, 3, "header + 2 groups")

	// group-1: 5 rules total, 2 firing, 1 pending, 2 inactive.
	assert.Regexp(t, `group-1\s+5\s+2\s+1\s+2`, lines[1])
}

func TestGroupsStatusTableCodec_InvalidType(t *testing.T) {
	codec := alert.GroupsStatusTable().Codec("table")
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not a slice")
	require.Error(t, err)
}
