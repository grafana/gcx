package experiments_test

import (
	"bytes"
	"encoding/json"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/experiments"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestSuitesTableGolden(t *testing.T) {
	var row experiments.TestSuite
	require.NoError(t, json.Unmarshal([]byte(`{"suite_id": "suite-1", "name": "Suite", "latest_version": "v2", "versions": [{"version": "v1"}, {"version": "v2"}], "tags": ["regression", "safety"], "created_at": "2026-09-01T12:30:00Z", "updated_at": "2026-09-02T13:45:00Z", "description": "description \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c"}`), &row))
	assertTableGolden(t, experiments.SuitesTable(), row, "table", "wide")
}

func TestCasesTableGolden(t *testing.T) {
	var row experiments.TestCase
	require.NoError(t, json.Unmarshal([]byte(`{"test_case_id": "case-1", "name": "Case", "category": "safety", "tags": ["regression", "safety"], "suite_id": "suite-1", "suite_version": "v2", "created_at": "2026-09-01T12:30:00Z", "updated_at": "2026-09-02T13:45:00Z", "description": "description \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c"}`), &row))
	assertTableGolden(t, experiments.CasesTable(), row, "table", "wide")
}

func TestTrialsTableGolden(t *testing.T) {
	var row experiments.TestCaseTrial
	require.NoError(t, json.Unmarshal([]byte(`{"trial_id": "trial-1", "experiment_id": "experiment-1", "test_case_id": "case-1", "attempt": 2, "status": "failed", "conversation_id": "conversation-1", "trace_id": "trace-1", "total_tokens": 123, "duration_ms": 456, "created_at": "2026-09-01T12:30:00Z", "completed_at": "2026-09-02T13:45:00Z", "error": "failure \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c"}`), &row))
	assertTableGolden(t, experiments.TrialsTable(), row, "table", "wide")
}

func TestArtifactsTableGolden(t *testing.T) {
	var row experiments.Artifact
	require.NoError(t, json.Unmarshal([]byte(`{"artifact_id": "artifact-1", "name": "Transcript", "kind": "transcript", "mime": "application/json", "parent_kind": "trial", "parent_id": "trial-1", "size_bytes": 1024, "created_at": "2026-09-01T12:30:00Z"}`), &row))
	assertTableGolden(t, experiments.ArtifactsTable(), row, "table", "wide")
}

func TestTableGolden(t *testing.T) {
	var row experiments.Experiment
	require.NoError(t, json.Unmarshal([]byte(`{"experiment_id": "experiment-1", "name": "Experiment", "status": "completed", "suite_id": "suite-1", "suite_version": "v2", "tags": ["regression", "safety"], "result": {"trial_count": 4, "pass_rate": 0.75}, "created_at": "2026-09-01T12:30:00Z", "completed_at": "2026-09-02T13:45:00Z", "description": "description \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "result_error": "rollup failure \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c"}`), &row))
	assertTableGolden(t, experiments.Table(), row, "table", "wide")
}

func TestScoresTableGolden(t *testing.T) {
	var row experiments.ScoreItem
	require.NoError(t, json.Unmarshal([]byte(`{"score_id": "score-1", "evaluator_id": "evaluator-1", "score_key": "quality", "value": {"number": 0.75}, "passed": true, "generation_id": "generation-1", "explanation": "explanation \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	assertTableGolden(t, experiments.ScoresTable(), row, "table", "wide")
}

func assertTableGolden[T any](t *testing.T, table cmdio.Table[T], row T, formats ...string) {
	t.Helper()
	var zero T
	for _, name := range formats {
		t.Run(name, func(t *testing.T) {
			for _, rows := range []struct {
				name  string
				items []T
			}{
				{"populated", []T{row, zero}},
				{"empty", []T{}},
				{"nil", nil},
			} {
				t.Run(rows.name, func(t *testing.T) {
					var buf bytes.Buffer
					require.NoError(t, table.Codec(name).Encode(&buf, rows.items))
					testutils.Golden(t, t.Name(), buf.String())
				})
			}
		})
	}
}
