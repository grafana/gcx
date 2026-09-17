package evaluators_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/evaluators"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row eval.EvaluatorDefinition
	require.NoError(t, json.Unmarshal([]byte(`{"evaluator_id": "evaluator-1", "version": "v1", "kind": "llm", "description": "description \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "output_keys": [{"key": "quality"}, {"key": "safety"}], "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	for _, tc := range []struct {
		name  string
		codec format.Codec
	}{
		{"table", evaluators.Table().Codec("table")},
		{"wide", evaluators.Table().Codec("wide")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, rows := range []struct {
				name  string
				items []eval.EvaluatorDefinition
			}{
				{"populated", []eval.EvaluatorDefinition{row, {}}},
				{"empty", []eval.EvaluatorDefinition{}},
				{"nil", nil},
			} {
				t.Run(rows.name, func(t *testing.T) {
					var buf bytes.Buffer
					require.NoError(t, tc.codec.Encode(&buf, rows.items))
					testutils.Golden(t, t.Name(), buf.String())
				})
			}
		})
	}
}
