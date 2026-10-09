package scores_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/scores"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScore_UnmarshalJSON_SourceShapes(t *testing.T) {
	t.Run("flat source_kind/source_id folds into nested Source", func(t *testing.T) {
		var s scores.Score
		require.NoError(t, json.Unmarshal([]byte(`{"score_id":"s-1","source_kind":"rule","source_id":"r-1"}`), &s))
		require.NotNil(t, s.Source)
		assert.Equal(t, "rule", s.Source.Kind)
		assert.Equal(t, "r-1", s.Source.ID)
		// Flat fields are cleared so re-marshaling emits a single shape.
		assert.Empty(t, s.SourceKind)
		assert.Empty(t, s.SourceID)

		out, err := json.Marshal(s)
		require.NoError(t, err)
		assert.Contains(t, string(out), `"source":{`)
		assert.NotContains(t, string(out), "source_kind")
		assert.NotContains(t, string(out), "source_id")
	})

	t.Run("nested source is preserved and not overwritten", func(t *testing.T) {
		var s scores.Score
		require.NoError(t, json.Unmarshal([]byte(`{"score_id":"s-1","source":{"kind":"generation","id":"g-1"}}`), &s))
		require.NotNil(t, s.Source)
		assert.Equal(t, "generation", s.Source.Kind)
		assert.Equal(t, "g-1", s.Source.ID)
	})

	t.Run("no provenance leaves Source nil", func(t *testing.T) {
		var s scores.Score
		require.NoError(t, json.Unmarshal([]byte(`{"score_id":"s-1"}`), &s))
		assert.Nil(t, s.Source)
	})
}

func TestScoreValue_Display(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	b := func(v bool) *bool { return &v }
	s := func(v string) *string { return &v }

	tests := []struct {
		name  string
		value scores.ScoreValue
		want  string
	}{
		{name: "number", value: scores.ScoreValue{Number: f(0.95)}, want: "0.95"},
		{name: "number integer", value: scores.ScoreValue{Number: f(1)}, want: "1"},
		{name: "bool true", value: scores.ScoreValue{Bool: b(true)}, want: "true"},
		{name: "bool false", value: scores.ScoreValue{Bool: b(false)}, want: "false"},
		{name: "string", value: scores.ScoreValue{String: s("good")}, want: "good"},
		{name: "empty", value: scores.ScoreValue{}, want: "-"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.value.Display())
		})
	}
}
