package rules_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/providers/sigil/eval/rules"
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
}
