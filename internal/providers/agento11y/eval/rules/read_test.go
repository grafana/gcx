package rules_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval/rules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_readRuleFile_YAMLErrorReported(t *testing.T) {
	content := "rule_id: my-rule\nselector:\n  - invalid:\n  bad indent"
	path := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	_, err := rules.ReadRuleFile(path, nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "looking for beginning of value")
}

func Test_readRuleFile_ValidYAML(t *testing.T) {
	content := `rule_id: my-rule
enabled: true
selector: user_visible_turn
sample_rate: 1.0
evaluator_ids:
  - eval-1
`
	path := filepath.Join(t.TempDir(), "rule.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	def, err := rules.ReadRuleFile(path, nil)
	require.NoError(t, err)
	assert.Equal(t, "my-rule", def.RuleID)
	assert.True(t, def.Enabled)
	// Generation-level rules carry no min_idle_seconds.
	assert.Nil(t, def.MinIdleSeconds)
}

func Test_readRuleFile_ConversationRuleMinIdleSeconds(t *testing.T) {
	content := `rule_id: online.conversation.report_compiler.response_quality
enabled: true
selector: conversation
min_idle_seconds: 10
match:
  agent_name:
    - Report Compiler
sample_rate: 1.0
evaluator_ids:
  - custom.response_quality.v1
`
	path := filepath.Join(t.TempDir(), "conversation-rule.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	def, err := rules.ReadRuleFile(path, nil)
	require.NoError(t, err)
	assert.Equal(t, "conversation", def.Selector)
	require.NotNil(t, def.MinIdleSeconds)
	assert.Equal(t, 10, *def.MinIdleSeconds)
}

func Test_readRuleFile_Actions(t *testing.T) {
	content := `rule_id: quality
enabled: true
selector: conversation
sample_rate: 1
evaluator_ids: [quality]
actions:
  - action_id: ra_existing
    enabled: false
    condition:
      kind: all_evaluators_fail
    action_config:
      kind: add_to_collection
      collection_ids: [needs-review, regression]
`
	path := filepath.Join(t.TempDir(), "rule-actions.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	def, err := rules.ReadRuleFile(path, nil)
	require.NoError(t, err)
	require.NotNil(t, def.Actions)
	require.Len(t, *def.Actions, 1)
	assert.Equal(t, "ra_existing", (*def.Actions)[0].ActionID)
	assert.Equal(t, "all_evaluators_fail", (*def.Actions)[0].Condition.Kind)
	assert.Equal(t, []string{"needs-review", "regression"}, (*def.Actions)[0].ActionConfig.CollectionIDs)
}
