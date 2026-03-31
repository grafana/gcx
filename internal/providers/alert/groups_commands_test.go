package alert_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupsTableCodec_Encode(t *testing.T) {
	codec := &alert.GroupsTableCodec{}
	assert.Equal(t, "table", string(codec.Format()))

	groups := []alert.RuleGroup{
		{Name: "group-1", FolderUID: "folder-abc", Interval: 60, Rules: make([]alert.RuleStatus, 3)},
		{Name: "group-2", FolderUID: "folder-xyz", Interval: 120, Rules: make([]alert.RuleStatus, 1)},
	}

	var buf bytes.Buffer
	err := codec.Encode(&buf, groups)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "FOLDER")
	assert.Contains(t, output, "RULES")
	assert.Contains(t, output, "INTERVAL")
	assert.Contains(t, output, "group-1")
	assert.Contains(t, output, "folder-abc")
	assert.Contains(t, output, "60s")
	assert.Contains(t, output, "120s")

	lines := strings.Split(strings.TrimSpace(output), "\n")
	require.Len(t, lines, 3, "header + 2 data rows")
}

func TestGroupsTableCodec_InvalidType(t *testing.T) {
	codec := &alert.GroupsTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not a slice")
	require.Error(t, err)
}

func TestGroupRulesTableCodec_Encode(t *testing.T) {
	codec := &alert.GroupRulesTableCodec{}
	assert.Equal(t, "table", string(codec.Format()))

	group := &alert.RuleGroup{
		Name: "test-group",
		Rules: []alert.RuleStatus{
			{UID: "uid-1", Name: "Rule 1", State: alert.StateFiring, Health: "ok", IsPaused: false},
			{UID: "uid-2", Name: "Rule 2", State: alert.StateInactive, Health: "ok", IsPaused: true},
		},
	}

	var buf bytes.Buffer
	err := codec.Encode(&buf, group)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "uid-1")
	assert.Contains(t, output, "uid-2")
	assert.Contains(t, output, alert.StateFiring)
	assert.Contains(t, output, alert.StateInactive)

	lines := strings.Split(strings.TrimSpace(output), "\n")
	require.Len(t, lines, 3, "header + 2 rules")
}

func TestGroupsStatusTableCodec_Encode(t *testing.T) {
	codec := &alert.GroupsStatusTableCodec{}
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
	codec := &alert.GroupsStatusTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not a slice")
	require.Error(t, err)
}
