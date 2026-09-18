package alert_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
