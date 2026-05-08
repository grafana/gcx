package output_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMutationResult_Emit_AgentMode_Changed(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	r := output.MutationResult{
		Action:  "configure",
		Target:  output.Target{Cluster: "prod-eu"},
		Changed: true,
		Fields:  []output.FieldChange{{Name: "costMetrics", From: "false", To: "true"}},
	}
	var buf bytes.Buffer
	require.NoError(t, r.Emit(&buf))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got), "output must be valid JSON: %s", buf.String())
	assert.Equal(t, "configure", got["action"])
	assert.Equal(t, true, got["changed"])
	fields, ok := got["fields"].([]any)
	require.True(t, ok)
	assert.Len(t, fields, 1)
}

func TestMutationResult_Emit_AgentMode_NoChange(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	r := output.MutationResult{
		Action:  "exclude",
		Target:  output.Target{Cluster: "prod-eu", Namespace: "checkout", Service: "payment-svc"},
		Changed: false,
	}
	var buf bytes.Buffer
	require.NoError(t, r.Emit(&buf))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, false, got["changed"])
	// fields key should not be present (omitempty)
	_, hasFields := got["fields"]
	assert.False(t, hasFields)
}

func TestMutationResult_Emit_HumanMode_Changed(t *testing.T) {
	agent.SetFlag(false)

	r := output.MutationResult{
		Action:  "remove",
		Target:  output.Target{Cluster: "prod-eu"},
		Changed: true,
	}
	var buf bytes.Buffer
	require.NoError(t, r.Emit(&buf))
	assert.Contains(t, buf.String(), "remove")
	assert.Contains(t, buf.String(), "prod-eu")
	assert.NotContains(t, buf.String(), "no changes")
}

func TestMutationResult_Emit_HumanMode_NoChange(t *testing.T) {
	agent.SetFlag(false)

	r := output.MutationResult{
		Action:  "exclude",
		Target:  output.Target{Cluster: "c", Namespace: "ns", Service: "svc"},
		Changed: false,
	}
	var buf bytes.Buffer
	require.NoError(t, r.Emit(&buf))
	assert.Contains(t, buf.String(), "no changes")
}
