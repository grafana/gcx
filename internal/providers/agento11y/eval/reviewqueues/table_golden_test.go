package reviewqueues_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval/reviewqueues"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row reviewqueues.ReviewQueue
	require.NoError(t, json.Unmarshal([]byte(`{"collection_id":"col-1","name":"Regression","rubric":[{"evaluator_id":"helpful","evaluator_version":"v1","output_key":{"key":"helpful","type":"number"}},{"evaluator_id":"safe","evaluator_version":"v2","output_key":{"key":"safe","type":"bool"}}],"total":3,"reviewed":1,"pending":2,"next_conversation_id":"conv-2","truncated":true}`), &row))
	testutils.AssertTableGolden(t, reviewqueues.Table(), row, "table", "wide")
}

func TestScoresTableGolden(t *testing.T) {
	row := reviewqueues.ScoreRow{ConversationID: "conv-1", GenerationID: "gen-1", AgentName: "assistant", AgentVersion: "v2", EvaluatorID: "helpful", EvaluatorVersion: "v1", ScoreKey: "helpful", Value: "4", Passed: "true", Reviewer: "alice", Note: "clear and correct", CreatedAt: "2026-09-01T12:00:00Z"}
	testutils.AssertTableGolden(t, reviewqueues.ScoresTable(), row, "table", "wide")
}
