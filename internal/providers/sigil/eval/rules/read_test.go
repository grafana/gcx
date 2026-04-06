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

func Test_toJSON_RejectsNonObjectJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "array", input: `[{"rule_id":"x"}]`},
		{name: "string", input: `"hello"`},
		{name: "number", input: `42`},
		{name: "null", input: `null`},
		{name: "boolean", input: `true`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := rules.ToJSON([]byte(tc.input))
			require.Error(t, err, "toJSON should reject non-object JSON: %s", tc.input)
		})
	}
}

func Test_toJSON_AcceptsValidObject(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "json object", input: `{"sample_rate": 0.5}`},
		{name: "yaml object", input: "sample_rate: 0.5\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := rules.ToJSON([]byte(tc.input))
			require.NoError(t, err)
			assert.NotEmpty(t, out)
		})
	}
}
