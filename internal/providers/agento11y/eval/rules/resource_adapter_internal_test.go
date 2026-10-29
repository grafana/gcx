package rules

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpecToUnstructured_KeepsExecutionModeAndFilterableTagKeys pins the
// `rules get` and `rules list` output: both fields stay in the spec, so a
// pulled rule can be pushed back without losing them.
func TestSpecToUnstructured_KeepsExecutionModeAndFilterableTagKeys(t *testing.T) {
	u, err := specToUnstructured(eval.RuleDefinition{
		RuleID:            "gated_quality",
		Enabled:           true,
		Selector:          "user_visible_turn",
		SampleRate:        1.0,
		EvaluatorIDs:      []string{"cheap_gate", "quality_judge"},
		ExecutionMode:     "sequential",
		FilterableTagKeys: []string{"environment", "team"},
	}, "default")
	require.NoError(t, err)

	spec, ok := u.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "sequential", spec["execution_mode"])
	assert.Equal(t, []any{"environment", "team"}, spec["filterable_tag_keys"])
}
