package guards_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/guards"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row eval.HookRuleDefinition
	require.NoError(t, json.Unmarshal([]byte(`{"rule_id": "guard-1", "enabled": true, "phase": "request", "priority": 10, "selector": "{agent=\"assistant\"}", "action_on_fail": "warn", "evaluator_ids": ["quality", "safety"], "redact": {"patterns": [{"regex": "secret"}]}, "tool_filter": {"blocked_names": ["exec"]}, "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	testutils.AssertTableGolden(t, guards.Table(), row, "table", "wide")
}
