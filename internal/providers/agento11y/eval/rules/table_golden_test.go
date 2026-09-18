package rules_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/rules"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row eval.RuleDefinition
	require.NoError(t, json.Unmarshal([]byte(`{"rule_id": "rule-1", "enabled": true, "selector": "{agent=\"assistant\"}", "sample_rate": 0.25, "evaluator_ids": ["quality", "safety"], "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	testutils.AssertTableGolden(t, rules.Table(), row, "table", "wide")
}
