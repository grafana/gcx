package alert_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstancesTableCodec_Encode(t *testing.T) {
	codec := &alert.InstancesTableCodec{}
	assert.Equal(t, "table", string(codec.Format()))

	instances := []alert.AlertInstanceRecord{
		{
			RuleUID:  "rule-1",
			RuleName: "CPU High",
			State:    alert.StateFiring,
			ActiveAt: "2026-04-08T12:34:56Z",
			Value:    91.5,
			Labels: map[string]string{
				"instance": "api-1",
			},
		},
		{
			RuleUID:  "rule-2",
			RuleName: "Disk Full",
			State:    alert.StatePending,
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
	assert.Contains(t, output, "LABELS")
	assert.Contains(t, output, "rule-1")
	assert.Contains(t, output, "CPU High")
	assert.Contains(t, output, "91.5")
	assert.Contains(t, output, "instance=api-1")
	assert.Contains(t, output, "-", "missing values should render as dash")
}

func TestInstancesTableCodec_EncodeWide(t *testing.T) {
	codec := &alert.InstancesTableCodec{Wide: true}
	assert.Equal(t, "wide", string(codec.Format()))

	instances := []alert.AlertInstanceRecord{
		{
			RuleUID:   "rule-1",
			RuleName:  "CPU High",
			GroupName: "platform",
			FolderUID: "folder-a",
			State:     alert.StateFiring,
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
	codec := &alert.InstancesTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not instances")
	require.Error(t, err)
}

func TestInstancesTableCodec_Decode(t *testing.T) {
	codec := &alert.InstancesTableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}
