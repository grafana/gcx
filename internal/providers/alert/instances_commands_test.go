package alert

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstancesTableCodec_Encode(t *testing.T) {
	codec := &InstancesTableCodec{}
	assert.Equal(t, "table", string(codec.Format()))

	instances := []AlertInstanceRecord{
		{
			RuleUID:  "rule-1",
			RuleName: "CPU High",
			State:    StateFiring,
			ActiveAt: "2026-04-08T12:34:56Z",
			Value:    91.5,
		},
		{
			RuleUID:  "rule-2",
			RuleName: "Disk Full",
			State:    StatePending,
		},
	}

	var buf bytes.Buffer
	err := codec.Encode(&buf, instances)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "RULE_UID")
	assert.Contains(t, output, "RULE")
	assert.Contains(t, output, "STATE")
	assert.Contains(t, output, "ACTIVE_AT")
	assert.Contains(t, output, "VALUE")
	assert.Contains(t, output, "rule-1")
	assert.Contains(t, output, "CPU High")
	assert.Contains(t, output, "91.5")
	assert.Contains(t, output, "-", "missing values should render as dash")
}

func TestInstancesTableCodec_EncodeWide(t *testing.T) {
	codec := &InstancesTableCodec{Wide: true}
	assert.Equal(t, "wide", string(codec.Format()))

	instances := []AlertInstanceRecord{
		{
			RuleUID:   "rule-1",
			RuleName:  "CPU High",
			GroupName: "platform",
			FolderUID: "folder-a",
			State:     StateFiring,
			ActiveAt:  "2026-04-08T12:34:56Z",
			Value:     99,
			Labels: map[string]string{
				"instance": "api-1",
				"severity": "critical",
			},
		},
	}

	var buf bytes.Buffer
	err := codec.Encode(&buf, instances)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "GROUP")
	assert.Contains(t, output, "FOLDER")
	assert.Contains(t, output, "LABELS")
	assert.Contains(t, output, "instance=api-1")
	assert.Contains(t, output, "severity=critical")
}

func TestInstancesTableCodec_InvalidType(t *testing.T) {
	codec := &InstancesTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not instances")
	require.Error(t, err)
}

func TestInstancesTableCodec_Decode(t *testing.T) {
	codec := &InstancesTableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}

func TestCollectAlertInstances_FiltersByState(t *testing.T) {
	groups := []RuleGroup{
		{
			Name:      "g1",
			FolderUID: "folder-1",
			Rules: []RuleStatus{
				{
					UID:   "rule-1",
					Name:  "CPU High",
					State: StateFiring,
					Alerts: []AlertInstance{
						{State: StateFiring, Value: 90, ActiveAt: "2026-04-08T10:00:00Z"},
						{State: StatePending, Value: 80, ActiveAt: "2026-04-08T10:01:00Z"},
					},
				},
				{
					UID:   "rule-2",
					Name:  "Memory High",
					State: StateFiring,
					Alerts: []AlertInstance{
						{State: "", Value: 95}, // falls back to rule state
					},
				},
			},
		},
	}

	all := collectAlertInstances(groups, "")
	require.Len(t, all, 3)

	firing := collectAlertInstances(groups, StateFiring)
	require.Len(t, firing, 2)
	for _, inst := range firing {
		assert.Equal(t, StateFiring, inst.State)
	}
}

func TestValidateAlertState(t *testing.T) {
	require.NoError(t, validateAlertState(""))
	require.NoError(t, validateAlertState(StateFiring))
	require.NoError(t, validateAlertState(StatePending))
	require.NoError(t, validateAlertState(StateInactive))

	err := validateAlertState("broken")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "invalid state"))
}
