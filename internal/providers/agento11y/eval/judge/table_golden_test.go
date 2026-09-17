package judge_test

import (
	"bytes"
	"encoding/json"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/judge"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestProvidersTableGolden(t *testing.T) {
	var row eval.JudgeProvider
	require.NoError(t, json.Unmarshal([]byte(`{"id": "provider-1", "name": "Provider", "type": "openai"}`), &row))
	assertTableGolden(t, judge.ProvidersTable(), row, "table")
}

func TestModelsTableGolden(t *testing.T) {
	var row eval.JudgeModel
	require.NoError(t, json.Unmarshal([]byte(`{"id": "model-1", "name": "Judge", "provider": "provider-1", "context_window": 32000}`), &row))
	assertTableGolden(t, judge.ModelsTable(), row, "table")
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
