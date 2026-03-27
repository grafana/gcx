package alert_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRulesTableCodec_Encode(t *testing.T) {
	codec := &alert.RulesTableCodec{}
	assert.Equal(t, "table", string(codec.Format()))

	rules := []alert.RuleStatus{
		{UID: "uid-1", Name: "Rule 1", State: alert.StateFiring, Health: "ok", IsPaused: false},
		{UID: "uid-2", Name: "Rule 2", State: alert.StateInactive, Health: "ok", IsPaused: true},
	}

	var buf bytes.Buffer
	err := codec.Encode(&buf, rules)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "UID")
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "STATE")
	assert.Contains(t, output, "HEALTH")
	assert.Contains(t, output, "PAUSED")
	assert.NotContains(t, output, "LAST_EVAL", "default mode should not show LAST_EVAL")

	assert.Contains(t, output, "uid-1")
	assert.Contains(t, output, "Rule 1")
	assert.Contains(t, output, alert.StateFiring)

	lines := strings.Split(strings.TrimSpace(output), "\n")
	require.Len(t, lines, 3, "header + 2 data rows")
	assert.Contains(t, lines[1], "no")
	assert.Contains(t, lines[2], "yes")
}

func TestRulesTableCodec_Encode_Wide(t *testing.T) {
	codec := &alert.RulesTableCodec{Wide: true}
	assert.Equal(t, "wide", string(codec.Format()))

	rules := []alert.RuleStatus{
		{
			UID:            "uid-1",
			Name:           "Rule 1",
			State:          alert.StateFiring,
			Health:         "ok",
			IsPaused:       false,
			FolderUID:      "folder-abc",
			LastEvaluation: "2024-01-01T00:00:00Z",
			EvaluationTime: 0.123,
		},
		{
			UID:            "uid-2",
			Name:           "Rule 2",
			State:          alert.StateInactive,
			Health:         "ok",
			IsPaused:       false,
			FolderUID:      "folder-xyz",
			LastEvaluation: "0001-01-01T00:00:00Z",
			EvaluationTime: 0.0,
		},
	}

	var buf bytes.Buffer
	err := codec.Encode(&buf, rules)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "LAST_EVAL")
	assert.Contains(t, output, "EVAL_TIME")
	assert.Contains(t, output, "FOLDER")
	assert.Contains(t, output, "folder-abc")
	assert.Contains(t, output, "folder-xyz")
	assert.Contains(t, output, "never", "zero time should render as 'never'")
	assert.Contains(t, output, "0.123s")
}

func TestRulesTableCodec_InvalidType(t *testing.T) {
	codec := &alert.RulesTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not a slice")
	require.Error(t, err)
}

func TestRulesTableCodec_Decode(t *testing.T) {
	codec := &alert.RulesTableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}

func TestRuleDetailTableCodec_Encode(t *testing.T) {
	codec := &alert.RuleDetailTableCodec{}
	assert.Equal(t, "table", string(codec.Format()))

	rule := &alert.RuleStatus{
		UID:      "uid-1",
		Name:     "My Rule",
		State:    alert.StatePending,
		Health:   "ok",
		IsPaused: true,
	}

	var buf bytes.Buffer
	err := codec.Encode(&buf, rule)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "uid-1")
	assert.Contains(t, output, "My Rule")
	assert.Contains(t, output, alert.StatePending)
	assert.Contains(t, output, "yes")
}

func TestRuleDetailTableCodec_InvalidType(t *testing.T) {
	codec := &alert.RuleDetailTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, []alert.RuleStatus{})
	require.Error(t, err)
}
