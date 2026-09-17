package rules_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/rules"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row eval.RuleDefinition
	require.NoError(t, json.Unmarshal([]byte(`{"rule_id": "rule-1", "enabled": true, "selector": "{agent=\"assistant\"}", "sample_rate": 0.25, "evaluator_ids": ["quality", "safety"], "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	for _, tc := range []struct {
		name  string
		codec format.Codec
	}{
		{"table", rules.Table().Codec("table")},
		{"wide", rules.Table().Codec("wide")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, rows := range []struct {
				name  string
				items []eval.RuleDefinition
			}{
				{"populated", []eval.RuleDefinition{row, {}}},
				{"empty", []eval.RuleDefinition{}},
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
