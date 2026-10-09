package evaluators_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/evaluators"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row eval.EvaluatorDefinition
	require.NoError(t, json.Unmarshal([]byte(`{"evaluator_id": "evaluator-1", "version": "v1", "kind": "llm", "description": "description \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "output_keys": [{"key": "quality"}, {"key": "safety"}], "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	testutils.AssertTableGolden(t, evaluators.Table(), row, "table", "wide")
}
