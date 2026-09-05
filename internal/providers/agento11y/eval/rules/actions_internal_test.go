package rules

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyActionFlags(t *testing.T) {
	rule := &eval.RuleDefinition{}
	require.NoError(t, applyActionFlags(rule, []string{"pass-a", "pass-b"}, []string{"fail-a"}))
	require.NotNil(t, rule.Actions)
	require.Len(t, *rule.Actions, 2)
	assert.Equal(t, "all_evaluators_pass", (*rule.Actions)[0].Condition.Kind)
	assert.Equal(t, []string{"pass-a", "pass-b"}, (*rule.Actions)[0].ActionConfig.CollectionIDs)
	assert.Equal(t, "all_evaluators_fail", (*rule.Actions)[1].Condition.Kind)

	withActions := &eval.RuleDefinition{Actions: rule.Actions}
	assert.ErrorContains(t, applyActionFlags(withActions, []string{"another"}, nil), "cannot be combined")
}

func TestReadActionFileDefaultsEnabled(t *testing.T) {
	action, err := readActionFile("-", bytes.NewBufferString(`
condition:
  kind: all_evaluators_pass
action_config:
  kind: add_to_collection
  collection_ids: [review]
`))
	require.NoError(t, err)
	assert.True(t, action.Enabled)
	assert.Equal(t, "all_evaluators_pass", action.Condition.Kind)
	assert.Equal(t, []string{"review"}, action.ActionConfig.CollectionIDs)
}
