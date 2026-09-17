package guards_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/guards"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row eval.HookRuleDefinition
	require.NoError(t, json.Unmarshal([]byte(`{"rule_id": "guard-1", "enabled": true, "phase": "request", "priority": 10, "selector": "{agent=\"assistant\"}", "action_on_fail": "warn", "evaluator_ids": ["quality", "safety"], "redact": {"patterns": [{"regex": "secret"}]}, "tool_filter": {"blocked_names": ["exec"]}, "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	for _, tc := range []struct {
		name  string
		codec format.Codec
	}{
		{"table", guards.Table().Codec("table")},
		{"wide", guards.Table().Codec("wide")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, rows := range []struct {
				name  string
				items []eval.HookRuleDefinition
			}{
				{"populated", []eval.HookRuleDefinition{row, {}}},
				{"empty", []eval.HookRuleDefinition{}},
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
