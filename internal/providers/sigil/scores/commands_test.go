package scores_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/sigil/scores"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCodec_Encode(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	b := func(v bool) *bool { return &v }

	now := time.Date(2026, 4, 2, 18, 30, 0, 0, time.UTC)
	items := []scores.Score{
		{
			ScoreKey: "relevance", ScoreType: "number",
			Value: scores.ScoreValue{Number: f(0.95)}, Passed: b(true),
			EvaluatorID: "eval-1", EvaluatorVersion: "1.0", RuleID: "rule-1",
			Explanation: "High relevance", CreatedAt: now,
		},
		{
			ScoreKey: "harmful", ScoreType: "bool",
			Value: scores.ScoreValue{Bool: b(false)}, Passed: b(false),
			EvaluatorID: "eval-2", EvaluatorVersion: "2.0",
			CreatedAt: now,
		},
		{
			ScoreKey: "sentiment", ScoreType: "string",
			Value:       scores.ScoreValue{},
			EvaluatorID: "eval-3",
		},
	}

	tests := []struct {
		name string
		wide bool
		want []string
	}{
		{
			name: "table format",
			wide: false,
			want: []string{"SCORE KEY", "VALUE", "PASSED", "EVALUATOR", "CREATED AT",
				"relevance", "0.95", "yes", "eval-1", "2026-04-02 18:30"},
		},
		{
			name: "wide includes VERSION, RULE, EXPLANATION",
			wide: true,
			want: []string{"TYPE", "VERSION", "RULE", "EXPLANATION",
				"number", "1.0", "rule-1", "High relevance"},
		},
		{
			name: "failed shows no",
			wide: false,
			want: []string{"harmful", "no"},
		},
		{
			name: "nil passed shows dash",
			wide: false,
			want: []string{"sentiment", "-"},
		},
		{
			name: "empty rule shows dash in wide",
			wide: true,
			want: []string{"eval-2", "-"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &scores.TableCodec{Wide: tc.wide}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, items))

			output := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestTableCodec_WrongType(t *testing.T) {
	codec := &scores.TableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []Score")
}

func TestTableCodec_Format(t *testing.T) {
	tests := []struct {
		wide   bool
		expect string
	}{
		{false, "table"},
		{true, "wide"},
	}
	for _, tc := range tests {
		codec := &scores.TableCodec{Wide: tc.wide}
		assert.Equal(t, tc.expect, string(codec.Format()))
	}
}

func TestTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &scores.TableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}
