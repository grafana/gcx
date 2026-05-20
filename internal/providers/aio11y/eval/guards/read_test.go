package guards_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/providers/aio11y/eval/guards"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_readHookRuleFile_YAMLErrorReported(t *testing.T) {
	content := "rule_id: my-guard\nselector:\n  - invalid:\n  bad indent"
	path := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	_, err := guards.ReadHookRuleFile(path, nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "looking for beginning of value")
}

func Test_readHookRuleFile_ValidYAML(t *testing.T) {
	content := `rule_id: my-guard
enabled: true
phase: preflight
priority: 10
selector: all
action_on_fail: warn
short_circuit: true
evaluator_ids:
  - eval-1
transform:
  patterns:
    - regex: secret
      replacement: "[REDACTED]"
`
	path := filepath.Join(t.TempDir(), "guard.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	def, err := guards.ReadHookRuleFile(path, nil)
	require.NoError(t, err)
	assert.Equal(t, "my-guard", def.RuleID)
	assert.True(t, def.Enabled)
	assert.Equal(t, "preflight", def.Phase)
	assert.Equal(t, 10, def.Priority)
	assert.Equal(t, "warn", def.ActionOnFail)
	assert.True(t, def.ShortCircuit)
	require.NotNil(t, def.Transform)
	require.Len(t, def.Transform.Patterns, 1)
	assert.Equal(t, "secret", def.Transform.Patterns[0].Regex)
}
