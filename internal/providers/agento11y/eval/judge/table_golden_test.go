package judge_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/judge"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestProvidersTableGolden(t *testing.T) {
	var row eval.JudgeProvider
	require.NoError(t, json.Unmarshal([]byte(`{"id": "provider-1", "name": "Provider", "type": "openai"}`), &row))
	testutils.AssertTableGolden(t, judge.ProvidersTable(), row, "table")
}

func TestModelsTableGolden(t *testing.T) {
	var row eval.JudgeModel
	require.NoError(t, json.Unmarshal([]byte(`{"id": "model-1", "name": "Judge", "provider": "provider-1", "context_window": 32000}`), &row))
	testutils.AssertTableGolden(t, judge.ModelsTable(), row, "table")
}
